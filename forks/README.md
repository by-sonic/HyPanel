# HyPanel forks — Hysteria2 restart-free user management (Ф1)

HyPanel needs two small patches to the embedded proxy core so that **adding,
disabling, banning, or deleting a Hysteria2 user is a DB write that takes effect
immediately — with no inbound restart and no dropped connections for the other
users.** Upstream sing-box / sing-quic don't expose the needed hooks, so we keep
the two patched files here and reconstruct the forks at build time.

> The committed `go.mod` still points at upstream so editors resolve normally.
> **You must run `forks/setup.sh` on the Linux build host before `./build.sh`** —
> otherwise the build fails (`core/endpoint.go` calls methods that exist only in
> the patched fork).

## What we patched and why

Upstream facts (verified against the pinned versions):

| Module | Pin | File |
|---|---|---|
| `github.com/sagernet/sing-box` | fork commit `78b2e12fbdd8` (go.mod replace) | `protocol/hysteria2/inbound.go` |
| `github.com/sagernet/sing-quic` | `v0.6.1` | `hysteria2/service.go` |

- sing-box's hysteria2 inbound has **no external/HTTP auth and no traffic-stats
  listener** — only an inline `users: [{name,password}]` list, applied once at
  construction. (The `auth.type:http` / `/traffic` / `/kick` API people refer to
  belongs to the *standalone* hysteria2 Go server, a different codebase.)
- `sing-quic`'s `Service.UpdateUsers` already rebuilds the live `userMap` without
  touching the QUIC listener — the exact restart-free primitive we want — but the
  inbound never calls it after startup, and the swap is **unsynchronized** against
  the per-connection `ServeHTTP` reader (a data race).

### Patch 1 — `sing-quic/hysteria2/service.go`
1. **Race fix:** guard `userMap` with a `sync.RWMutex` (write in `UpdateUsers`,
   read in `ServeHTTP`).
2. **Session registry + `RetainUsers`:** track authenticated sessions and add
   `RetainUsers(passwords)`, which force-disconnects any live session whose auth
   password is no longer in the set (instant ban/kick instead of waiting for the
   QUIC idle-timeout).

### Patch 2 — `sing-box/protocol/hysteria2/inbound.go`
1. **`Service[int]` → `Service[string]`:** store password → user **name** instead
   of password → positional index. Upstream resolved `metadata.User` via
   `userNameList[index]`, where `index` is captured at handshake; live-swapping
   that list would mis-attribute traffic or **panic** an in-flight connection on
   an out-of-range read. Keying on the name makes live updates safe and keeps the
   existing name-based traffic accounting (`core/tracker_stats.go` →
   `service/stats.go`) working unchanged.
2. Expose `(*Inbound).UpdateUsers` and `(*Inbound).RetainUsers` so the panel can
   drive the live update / kick.

Net effect on the panel side: `Core.UpdateInboundUsers(tag, names, passwords)`
does `UpdateUsers` + `RetainUsers`, and `InboundService.ApplyUserChanges` routes
hysteria2 inbounds to it (others still use `RestartInbounds`). Gated by the
`hy2LiveUpdate` setting (default `true`); set it `false` to fall back to the
legacy remove+add behaviour against an unpatched core.

## Build (Linux only — CGO + libcronet, per s-ui)

```bash
# from repo root, on the Ubuntu dev-VPS
bash forks/setup.sh        # clone upstream pins, overlay patches, repoint go.mod
go mod tidy                # reconcile go.sum for the local replaces
./build.sh                 # the normal s-ui CGO build (build tags + ldflags)
```

`setup.sh` rewrites the sing-box/sing-quic `replace` lines to `./forks/...` (it
makes go.mod dirty by design). To return to upstream: `git checkout go.mod`.

## Manual test plan (run on the VPS, no automated coverage yet)

These need a live QUIC client (Hiddify / sing-box / NekoBox) hitting a hysteria2
inbound. The point is to prove "no dropped connections" and "instant kick".

1. **Add user, no drop:** connect client A; while A is streaming (e.g. a speed
   test), create client B on the same hy2 inbound via the panel. Expect: A's
   connection is **uninterrupted**; B can connect immediately. (Pre-Ф1 this
   dropped A.)
2. **Disable = instant block + kick:** with A connected and streaming, disable A
   (or `POST .../ban` with `banned=true`). Expect: A's transfer **stops within a
   second** and A cannot reconnect; other clients on the inbound are unaffected.
3. **Unban:** `ban` with `banned=false` → A can connect again.
4. **Accounting intact:** confirm A/B up/down counters still increment correctly
   (name-based stats unchanged).
5. **Fallback:** set `hy2LiveUpdate=false`, repeat (1) — expect the old behaviour
   (A dropped on B's add) to confirm the fallback path still works.
6. **Race:** build/run with `-race` if feasible under a load test toggling users,
   to confirm the `userMap` guard holds.

## Re-syncing after a sing-box / sing-quic bump

1. Bump the pins in the root `go.mod` (and `SB_COMMIT` / `SQ_REF` in `setup.sh`).
2. Diff the new upstream `inbound.go` / `service.go` against the headers in
   `patched-files/` (search `HyPanel:`) and re-apply the marked changes.
3. Re-run the test plan.
