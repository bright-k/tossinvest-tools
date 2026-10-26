package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActualHistoryCountBoundaryAndSevenDays(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "state.sqlite3"), "mock")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec("INSERT INTO sync(alias) VALUES('primary')")
	if e != nil {
		t.Fatal(e)
	}
	stmt, e := tx.Prepare("INSERT INTO events(alias,received_ns,payload,size) VALUES('primary',?,x'78',1)")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 100001; i++ {
		if _, e = stmt.Exec(now.UnixNano()); e != nil {
			t.Fatal(e)
		}
	}
	stmt.Close()
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = s.Prune(now, DefaultHistory); e != nil {
		t.Fatal(e)
	}
	var n, water int64
	_ = s.DB.QueryRow("SELECT count(*) FROM events").Scan(&n)
	_ = s.DB.QueryRow("SELECT pruned_seq FROM sync WHERE alias='primary'").Scan(&water)
	if n != 100000 || water != 1 {
		t.Fatal(n, water)
	}
	if e = s.Prune(now.Add(7*24*time.Hour), DefaultHistory); e != nil {
		t.Fatal(e)
	}
	_ = s.DB.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 0 {
		t.Fatal("7-day equality not expired")
	}
}
func TestPayloadPolicyBoundaryBackupRestoreAndCorruption(t *testing.T) {
	if DefaultHistory.Bytes != 256<<20 {
		t.Fatal("payload policy")
	}
	s, e := Open(filepath.Join(t.TempDir(), "state.sqlite3"), "mock")
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	p := HistoryPolicy{Age: 7 * 24 * time.Hour, PerAccount: 100000, Bytes: 256 << 10}
	payload := make([]byte, 1<<10)
	for i := 0; i < 256; i++ {
		if e = s.AppendEvent("a", now, payload, p); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	_ = s.DB.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 256 {
		t.Fatal(n)
	}
	if e = s.AppendEvent("b", now, []byte{1}, p); e != nil {
		t.Fatal(e)
	}
	var water int
	_ = s.DB.QueryRow("SELECT pruned_seq FROM sync WHERE alias='a'").Scan(&water)
	if water != 1 {
		t.Fatal("payload limit did not delete global prefix")
	}
	dest := filepath.Join(t.TempDir(), "backup.sqlite3")
	if e = s.Backup(dest); e != nil {
		t.Fatal(e)
	}
	if e = s.Check(); e != nil {
		t.Fatal(e)
	}
	s.Close()
	restored, e := Open(dest, "mock")
	if e != nil {
		t.Fatal(e)
	}
	if e = restored.Check(); e != nil {
		t.Fatal(e)
	}
	var gap bool
	_ = restored.DB.QueryRow("SELECT gap FROM sync WHERE alias='a'").Scan(&gap)
	restored.Close()
	if !gap {
		t.Fatal("restore assumed uninterrupted observation")
	}
	if _, e = Open(dest, "toss"); e == nil {
		t.Fatal("cross-backend DB opened")
	}
	bad := filepath.Join(t.TempDir(), "bad.sqlite3")
	os.WriteFile(bad, []byte("corrupt"), 0600)
	if _, e = Open(bad, "mock"); e == nil {
		t.Fatal("corrupt DB accepted")
	}
}

func TestActual256MiBPayloadBoundary(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "history.sqlite3"), "mock")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now()
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec("INSERT INTO sync(alias,high_seq) VALUES('primary',256)"); e != nil {
		t.Fatal(e)
	}
	stmt, e := tx.Prepare("INSERT INTO events(alias,received_ns,payload,size) VALUES('primary',?,?,?)")
	if e != nil {
		t.Fatal(e)
	}
	payload := make([]byte, 1<<20)
	for i := 0; i < 256; i++ {
		if _, e = stmt.Exec(now.UnixNano(), payload, len(payload)); e != nil {
			t.Fatal(e)
		}
	}
	stmt.Close()
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = s.Prune(now, DefaultHistory); e != nil {
		t.Fatal(e)
	}
	var bytes, water int64
	_ = s.DB.QueryRow("SELECT sum(size) FROM events").Scan(&bytes)
	_ = s.DB.QueryRow("SELECT pruned_seq FROM sync WHERE alias='primary'").Scan(&water)
	if bytes != 256<<20 || water != 0 {
		t.Fatal("equality pruned", bytes, water)
	}
	if e = s.AppendEvent("primary", now, []byte{1}, DefaultHistory); e != nil {
		t.Fatal(e)
	}
	_ = s.DB.QueryRow("SELECT sum(size) FROM events").Scan(&bytes)
	_ = s.DB.QueryRow("SELECT pruned_seq FROM sync WHERE alias='primary'").Scan(&water)
	if bytes != (255<<20)+1 || water != 1 {
		t.Fatal("actual payload overflow did not delete oldest prefix", bytes, water)
	}
}
