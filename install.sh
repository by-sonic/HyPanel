#!/usr/bin/env bash
# HyPanel one-command installer — Hysteria2-first VPN/proxy panel.
#   bash <(curl -fsSL https://raw.githubusercontent.com/by-sonic/HyPanel/main/install.sh)
# Idempotent: re-running repairs/updates the deployment.
set -eu
# NOTE: deliberately NOT using `pipefail` — `tr </dev/urandom | head -c N` makes
# head close the pipe early, tr gets SIGPIPE, and under pipefail+errexit that would
# silently abort the whole script at the random-credential step.

# ---- config (override via env) ----
HYPANEL_DATA="${HYPANEL_DATA:-/opt/hypanel}"
HYPANEL_CONTAINER="${HYPANEL_CONTAINER:-hypanel}"
HYPANEL_PANEL_PORT="${HYPANEL_PANEL_PORT:-2095}"
HYPANEL_SUB_PORT="${HYPANEL_SUB_PORT:-2096}"
HYPANEL_REPO="${HYPANEL_REPO:-https://github.com/by-sonic/HyPanel}"
GHCR_IMAGE="${HYPANEL_GHCR_IMAGE:-ghcr.io/by-sonic/hypanel:latest}"
WD_FLAG="/tmp/.hypanel_ufw_watchdog.pid"

c_info='\033[1;36m'; c_warn='\033[1;33m'; c_err='\033[1;31m'; c_ok='\033[1;32m'; c_off='\033[0m'
log()  { printf "${c_info}[HyPanel]${c_off} %s\n" "$*"; }
ok()   { printf "${c_ok}[HyPanel]${c_off} %s\n" "$*"; }
warn() { printf "${c_warn}[HyPanel]${c_off} %s\n" "$*"; }
err()  { printf "${c_err}[HyPanel]${c_off} %s\n" "$*" >&2; }

require_root() { [ "$(id -u)" -eq 0 ] || { err "Please run as root."; exit 1; }; }

install_docker() {
  if command -v docker >/dev/null 2>&1; then
    log "Docker present ($(docker --version | awk '{print $3}' | tr -d ,))."
  else
    log "Installing Docker..."
    curl -fsSL https://get.docker.com | sh
  fi
  systemctl enable --now docker >/dev/null 2>&1 || true
}

resolve_image() {
  if [ -n "${HYPANEL_IMAGE:-}" ]; then IMAGE="$HYPANEL_IMAGE"; log "Using image $IMAGE (override)."; return; fi
  log "Fetching HyPanel image..."
  if docker pull "$GHCR_IMAGE" >/dev/null 2>&1; then IMAGE="$GHCR_IMAGE"; ok "Pulled $IMAGE."; return; fi
  if docker image inspect hypanel:dev >/dev/null 2>&1; then IMAGE="hypanel:dev"; warn "Registry unavailable; using local hypanel:dev."; return; fi
  warn "No prebuilt image available; building from source (this is slow on small VPS)..."
  local tmp; tmp="$(mktemp -d)"
  git clone --depth 1 "$HYPANEL_REPO" "$tmp/src" >/dev/null 2>&1
  ( cd "$tmp/src" && docker build -t hypanel:local . )
  IMAGE="hypanel:local"; rm -rf "$tmp"; ok "Built $IMAGE."
}

run_panel() {
  mkdir -p "$HYPANEL_DATA/db" "$HYPANEL_DATA/cert"
  docker rm -f "$HYPANEL_CONTAINER" >/dev/null 2>&1 || true
  log "Starting container '$HYPANEL_CONTAINER'..."
  docker run -d --name "$HYPANEL_CONTAINER" --restart=unless-stopped \
    -p "${HYPANEL_PANEL_PORT}:2095" \
    -p "${HYPANEL_SUB_PORT}:2096" \
    -p 443:443 -p 80:80 \
    -v "$HYPANEL_DATA/db:/app/db" \
    -v "$HYPANEL_DATA/cert:/root/cert" \
    "$IMAGE" >/dev/null
}

wait_for_db() {
  log "Waiting for panel to initialize..."
  for _ in $(seq 1 40); do
    if docker exec "$HYPANEL_CONTAINER" test -f /app/db/hypanel.db >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  warn "DB not detected after 40s; continuing anyway."
}

rand() { LC_ALL=C tr -dc "$1" </dev/urandom 2>/dev/null | head -c "$2" || true; }

configure_panel() {
  # Fresh install (no prior creds marker) => generate strong random admin + obscure path.
  if [ -f "$HYPANEL_DATA/.access" ]; then
    log "Existing install detected; keeping current credentials/path."
    return
  fi
  ADMIN_USER="admin"
  ADMIN_PASS="$(rand 'A-Za-z0-9' 18)"
  PANEL_PATH="/$(rand 'a-z0-9' 12)/"
  log "Applying random admin password and panel path..."
  docker exec "$HYPANEL_CONTAINER" ./hypanel admin -username "$ADMIN_USER" -password "$ADMIN_PASS" >/dev/null 2>&1 || warn "could not set admin creds"
  docker exec "$HYPANEL_CONTAINER" ./hypanel setting -path "$PANEL_PATH" >/dev/null 2>&1 || warn "could not set panel path"
  docker restart "$HYPANEL_CONTAINER" >/dev/null 2>&1 || true
  umask 077
  cat > "$HYPANEL_DATA/.access" <<EOF
HYPANEL_ADMIN_USER=$ADMIN_USER
HYPANEL_ADMIN_PASS=$ADMIN_PASS
HYPANEL_PANEL_PATH=$PANEL_PATH
EOF
}

# Anti-lockout firewall: allow SSH FIRST, arm a detached watchdog that disables
# ufw after 300s unless this script cancels it (so a dropped SSH session can't
# permanently lock the operator out), then enable.
setup_firewall() {
  if ! command -v ufw >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get install -y ufw >/dev/null 2>&1 || { warn "ufw unavailable; skipping firewall."; return; }
  fi
  log "Configuring firewall (SSH-safe)..."
  ufw allow 22/tcp        >/dev/null 2>&1 || true
  ufw allow OpenSSH       >/dev/null 2>&1 || true
  ufw allow "${HYPANEL_PANEL_PORT}"/tcp >/dev/null 2>&1 || true
  ufw allow "${HYPANEL_SUB_PORT}"/tcp   >/dev/null 2>&1 || true
  ufw allow 80/tcp        >/dev/null 2>&1 || true
  ufw allow 443/tcp       >/dev/null 2>&1 || true
  ufw allow 443/udp       >/dev/null 2>&1 || true   # Hysteria2 (QUIC/UDP)
  if ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw reload >/dev/null 2>&1 || true
    return
  fi
  warn "Enabling ufw with a 300s auto-disable safety net (anti-lockout)."
  nohup bash -c "sleep 300; ufw --force disable; rm -f '$WD_FLAG'" >/dev/null 2>&1 &
  echo $! > "$WD_FLAG"
  ufw --force enable >/dev/null 2>&1 || true
}

cancel_watchdog() {
  if [ -f "$WD_FLAG" ]; then
    kill "$(cat "$WD_FLAG")" >/dev/null 2>&1 || true
    rm -f "$WD_FLAG"
    log "Firewall confirmed; safety net cancelled."
  fi
}

# Prefer IPv4 (panel access + sslip.io TLS rely on the v4 address).
public_ip() {
  curl -4 -fsS --max-time 5 https://api.ipify.org 2>/dev/null \
    || curl -4 -fsS --max-time 5 https://ifconfig.me 2>/dev/null \
    || curl -4 -fsS --max-time 5 https://icanhazip.com 2>/dev/null \
    || ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}' \
    || hostname -I | awk '{print $1}'
}

summary() {
  local ip; ip="$(public_ip | tr -d '[:space:]')"
  # shellcheck disable=SC1090
  . "$HYPANEL_DATA/.access"
  echo
  ok "HyPanel is up."
  echo "  ──────────────────────────────────────────────"
  echo "   Panel URL : http://${ip}:${HYPANEL_PANEL_PORT}${HYPANEL_PANEL_PATH}"
  echo "   Username  : ${HYPANEL_ADMIN_USER}"
  echo "   Password  : ${HYPANEL_ADMIN_PASS}"
  echo "   Sub port  : ${HYPANEL_SUB_PORT}"
  echo "   Data dir  : ${HYPANEL_DATA}"
  echo "  ──────────────────────────────────────────────"
  echo "   Credentials saved to ${HYPANEL_DATA}/.access"
  # TODO(next iter): zero-DNS TLS via <ip>.sslip.io + Let's Encrypt -> https URL.
  echo
}

main() {
  require_root
  install_docker
  resolve_image
  run_panel
  wait_for_db
  configure_panel
  setup_firewall
  cancel_watchdog
  summary
}
main "$@"
