package service

// HyPanel Ф1 tests — the first automated coverage in the repo. They exercise the
// pure DB-driven pieces of the restart-free Hysteria2 user management:
//
//   - hy2Users:      the SQL that selects which clients an hy2 inbound should
//                    authenticate (enabled, not banned, has an hy2 password,
//                    attached to the inbound).
//   - BanClient:     the admin ban/unban state transition + audit Changes rows.
//   - ResetClients:  a banned client must NOT be resurrected by its periodic reset.
//
// These do not require a running sing-box core: corePtr is pointed at a non-nil
// but not-started *core.Core, so ApplyUserChanges short-circuits to a no-op.
// (The live user-map swap / instant kick is covered by the manual VPS test plan
// in forks/README.md, which needs a real QUIC client.)

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/by-sonic/HyPanel/core"
	"github.com/by-sonic/HyPanel/database"
	"github.com/by-sonic/HyPanel/database/model"
)

// setupTestDB points the package-global DB at a fresh temp SQLite file and runs
// migrations. Tests run sequentially, so reassigning the global between tests is
// safe.
func setupTestDB(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("InitDB(%s): %v", dbPath, err)
	}
}

func mustCreateClient(t *testing.T, c *model.Client) {
	t.Helper()
	if err := database.GetDB().Create(c).Error; err != nil {
		t.Fatalf("create client %q: %v", c.Name, err)
	}
}

// hy2Client builds a client whose Config carries an hysteria2 password and that
// is attached to the given inbound ids.
func hy2Client(name, password string, inbounds []uint, enable, banned bool) *model.Client {
	inb, _ := json.Marshal(inbounds)
	return &model.Client{
		Name:     name,
		Enable:   enable,
		Banned:   banned,
		Config:   json.RawMessage(`{"hysteria2":{"password":"` + password + `"}}`),
		Inbounds: inb,
	}
}

func containsUint(s []uint, v uint) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestHy2Users(t *testing.T) {
	setupTestDB(t)

	mustCreateClient(t, hy2Client("alice", "pw_alice", []uint{1}, true, false))     // include
	mustCreateClient(t, hy2Client("bob", "pw_bob", []uint{1}, false, false))        // disabled -> exclude
	mustCreateClient(t, hy2Client("carol", "pw_carol", []uint{1}, true, true))      // banned -> exclude
	mustCreateClient(t, hy2Client("erin", "pw_erin", []uint{2}, true, false))       // other inbound -> exclude
	mustCreateClient(t, hy2Client("frank", "pw_frank", []uint{1, 2}, true, false))  // multi-inbound -> include
	mustCreateClient(t, hy2Client("emptypw", "", []uint{1}, true, false))           // empty password -> exclude
	// no hysteria2 block at all -> json_extract is NULL -> excluded by the query.
	mustCreateClient(t, &model.Client{
		Name:     "dave",
		Enable:   true,
		Config:   json.RawMessage(`{"vmess":{"id":"abc"}}`),
		Inbounds: json.RawMessage(`[1]`),
	})

	svc := &InboundService{}
	names, passwords, err := svc.hy2Users(database.GetDB(), 1)
	if err != nil {
		t.Fatalf("hy2Users: %v", err)
	}
	if len(names) != len(passwords) {
		t.Fatalf("names/passwords length mismatch: %d vs %d", len(names), len(passwords))
	}

	got := make(map[string]string, len(names))
	for i, n := range names {
		got[n] = passwords[i]
	}
	want := map[string]string{"alice": "pw_alice", "frank": "pw_frank"}
	if len(got) != len(want) {
		t.Fatalf("got %d users %v, want %d %v", len(got), got, len(want), want)
	}
	for n, pw := range want {
		if got[n] != pw {
			t.Errorf("user %q: got password %q, want %q", n, got[n], pw)
		}
	}
}

func TestBanClient(t *testing.T) {
	setupTestDB(t)
	corePtr = &core.Core{} // non-nil, not running -> ApplyUserChanges is a no-op

	c := hy2Client("mallory", "pw_m", []uint{1}, true, false)
	mustCreateClient(t, c)

	cs := &ConfigService{}
	if err := cs.BanClient(c.Id, true, "admin"); err != nil {
		t.Fatalf("BanClient(ban): %v", err)
	}

	var banned model.Client
	if err := database.GetDB().First(&banned, c.Id).Error; err != nil {
		t.Fatalf("reload banned client: %v", err)
	}
	if !banned.Banned || banned.Enable || banned.BannedAt == 0 {
		t.Errorf("after ban: Banned=%v Enable=%v BannedAt=%d; want true, false, non-zero",
			banned.Banned, banned.Enable, banned.BannedAt)
	}

	var nBan int64
	database.GetDB().Model(&model.Changes{}).
		Where("action = ? AND key = ?", "ban", "clients").Count(&nBan)
	if nBan != 1 {
		t.Errorf("ban Changes recorded = %d, want 1", nBan)
	}

	if err := cs.BanClient(c.Id, false, "admin"); err != nil {
		t.Fatalf("BanClient(unban): %v", err)
	}

	var unbanned model.Client
	if err := database.GetDB().First(&unbanned, c.Id).Error; err != nil {
		t.Fatalf("reload unbanned client: %v", err)
	}
	if unbanned.Banned || !unbanned.Enable || unbanned.BannedAt != 0 {
		t.Errorf("after unban: Banned=%v Enable=%v BannedAt=%d; want false, true, 0",
			unbanned.Banned, unbanned.Enable, unbanned.BannedAt)
	}

	var nUnban int64
	database.GetDB().Model(&model.Changes{}).Where("action = ?", "unban").Count(&nUnban)
	if nUnban != 1 {
		t.Errorf("unban Changes recorded = %d, want 1", nUnban)
	}
}

func TestResetClientsRespectsBan(t *testing.T) {
	setupTestDB(t)
	now := time.Now().Unix()

	// Banned + disabled + due for periodic reset: must stay disabled and its
	// inbound must NOT be scheduled for a restart.
	banned := hy2Client("banned", "pw_b", []uint{10}, false, true)
	banned.AutoReset = true
	banned.ResetDays = 30
	banned.NextReset = now - 100
	mustCreateClient(t, banned)

	// Over-quota (disabled, not banned) + due: re-enabled and inbound returned.
	overQuota := hy2Client("overquota", "pw_o", []uint{20}, false, false)
	overQuota.AutoReset = true
	overQuota.ResetDays = 30
	overQuota.NextReset = now - 100
	mustCreateClient(t, overQuota)

	svc := &ClientService{}
	inboundIds, err := svc.ResetClients(database.GetDB(), now)
	if err != nil {
		t.Fatalf("ResetClients: %v", err)
	}

	var b model.Client
	database.GetDB().First(&b, banned.Id)
	if b.Enable {
		t.Errorf("banned client was re-enabled by reset; want still disabled")
	}
	if containsUint(inboundIds, 10) {
		t.Errorf("banned client's inbound 10 returned for restart; want excluded")
	}

	var o model.Client
	database.GetDB().First(&o, overQuota.Id)
	if !o.Enable {
		t.Errorf("over-quota client was not re-enabled by reset; want enabled")
	}
	if !containsUint(inboundIds, 20) {
		t.Errorf("over-quota client's inbound 20 missing from restart set; want included")
	}
}
