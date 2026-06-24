package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// deadPID is a pid that is essentially guaranteed not to exist, used to assert
// the liveness filter drops stale rows.
const deadPID = 0x3FFFFFFF

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := OpenAt(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mk(agent, session, cwd string, pid int, status string, at int64) Status {
	return Status{Agent: agent, SessionID: session, Cwd: cwd, Pid: pid, Status: status, UpdatedAt: at, Event: status}
}

func TestMigrateSetsVersionAndIsIdempotent(t *testing.T) {
	db := openTemp(t)
	var v int
	if err := db.sql.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != schemaVersion {
		t.Fatalf("user_version = %d, want %d", v, schemaVersion)
	}
	if err := db.Migrate(); err != nil { // second call must be a no-op
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestUpsertNewestPerCwdWins(t *testing.T) {
	db := openTemp(t)
	self := os.Getpid()
	if err := db.Upsert(mk("claude", "s1", "/work", self, Running, 100)); err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert(mk("codex", "s2", "/work", self, Waiting, 200)); err != nil {
		t.Fatal(err)
	}
	live, err := db.LiveByCwd()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := live["/work"]
	if !ok {
		t.Fatal("no live row for /work")
	}
	if got.SessionID != "s2" || got.Status != Waiting {
		t.Fatalf("newest-wins failed: got %+v", got)
	}
}

func TestUpsertConflictUpdatesInPlace(t *testing.T) {
	db := openTemp(t)
	self := os.Getpid()
	// First turn: prompt captured, running.
	db.Upsert(Status{Agent: "claude", SessionID: "s", Cwd: "/w", Pid: self, Status: Running, Prompt: "do the thing", UpdatedAt: 1})
	// Later: idle, no new prompt, fresh latest message + model.
	db.Upsert(Status{Agent: "claude", SessionID: "s", Cwd: "/w", Pid: self, Status: Idle, Latest: "done", Model: "opus", UpdatedAt: 2})

	all, err := db.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("want 1 row, got %d", len(all))
	}
	r := all[0]
	if r.Status != Idle || r.Prompt != "do the thing" || r.Latest != "done" || r.Model != "opus" {
		t.Fatalf("conflict merge wrong: %+v", r)
	}
}

func TestDelete(t *testing.T) {
	db := openTemp(t)
	db.Upsert(mk("claude", "s", "/w", os.Getpid(), Running, 1))
	if err := db.Delete("claude", "s"); err != nil {
		t.Fatal(err)
	}
	all, _ := db.All()
	if len(all) != 0 {
		t.Fatalf("want 0 rows after delete, got %d", len(all))
	}
	if err := db.Delete("claude", "s"); err != nil { // deleting missing row is fine
		t.Fatalf("delete missing: %v", err)
	}
}

func TestLiveByCwdFiltersAndReapsDeadPID(t *testing.T) {
	db := openTemp(t)
	db.Upsert(mk("claude", "alive", "/a", os.Getpid(), Running, 1))
	db.Upsert(mk("claude", "dead", "/b", deadPID, Running, 1))

	live, err := db.LiveByCwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := live["/a"]; !ok {
		t.Fatal("live row /a missing")
	}
	if _, ok := live["/b"]; ok {
		t.Fatal("dead row /b should be filtered")
	}
	// dead row should have been reaped from the table
	all, _ := db.All()
	for _, r := range all {
		if r.SessionID == "dead" {
			t.Fatal("dead row was not reaped")
		}
	}
}

func TestUnknownPidTreatedLive(t *testing.T) {
	db := openTemp(t)
	db.Upsert(mk("codex", "s", "/c", 0, Waiting, 1)) // pid 0 = unknown
	live, _ := db.LiveByCwd()
	if _, ok := live["/c"]; !ok {
		t.Fatal("pid 0 (unknown) should be treated as live")
	}
}

func TestTruncateBoundsField(t *testing.T) {
	db := openTemp(t)
	big := make([]byte, maxField+500)
	for i := range big {
		big[i] = 'x'
	}
	db.Upsert(Status{Agent: "claude", SessionID: "s", Cwd: "/w", Pid: 0, Status: Running, Latest: string(big), UpdatedAt: 1})
	all, _ := db.All()
	if len(all[0].Latest) > maxField {
		t.Fatalf("latest not truncated: %d", len(all[0].Latest))
	}
}

func TestConcurrentUpsert(t *testing.T) {
	db := openTemp(t)
	self := os.Getpid()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := fmt.Sprintf("sess-%d", i)
			if err := db.Upsert(mk("claude", s, "/w"+s, self, Running, int64(i))); err != nil {
				t.Errorf("concurrent upsert %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	all, err := db.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 50 {
		t.Fatalf("want 50 rows, got %d", len(all))
	}
}

func TestPathHonorsOverrideAndXDG(t *testing.T) {
	t.Setenv("TWM_DB_PATH", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "agents.db" {
		t.Fatalf("unexpected path: %s", p)
	}
	override := filepath.Join(t.TempDir(), "custom", "x.db")
	t.Setenv("TWM_DB_PATH", override)
	p, err = Path()
	if err != nil || p != override {
		t.Fatalf("override not honored: %s err=%v", p, err)
	}
}
