package core

import (
	"context"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/store"
	"testing"
	"time"
)

func TestCursorRetentionRevisionRestartTamper(t *testing.T) {
	s, _ := newService(t)
	grantAccounts(t, s, 2)
	now := time.Now().UTC()
	s.Now = func() time.Time { return now }
	payload := []byte(`{"event":"NEW_EVENT","accountSeq":"1","order":{"orderId":"mock","quantity":"0.0000001","unknown":"keep"}}`)
	read := func(args Object) (Object, error) { return s.Events(context.Background(), DefaultPrincipal(), args) }
	first, e := read(Object{"account_alias": "primary"})
	if e != nil {
		t.Fatal(e)
	}
	emptyCursor := contract.Str(contract.Obj(first["data"])["next_cursor"])
	if e = s.Store.AppendEvent("primary", now.Add(-7*24*time.Hour), payload, store.HistoryPolicy{Age: 8 * 24 * time.Hour, PerAccount: 100000, Bytes: 256 << 20}); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.AppendEvent("primary", now, payload, store.DefaultHistory); e != nil {
		t.Fatal(e)
	}
	_, e = read(Object{"account_alias": "primary", "cursor": emptyCursor})
	assertCode(t, e, "CURSOR_EXPIRED")
	rev, _ := s.Revision()
	atWatermark := s.signCursor(cursor{s.Backend, s.RunID, PrincipalID, rev, "primary", 1})
	page, e := read(Object{"account_alias": "primary", "cursor": atWatermark})
	if e != nil {
		t.Fatal(e)
	}
	events := contract.List(contract.Obj(page["data"])["events"])
	if len(events) != 1 || contract.Obj(contract.Obj(events[0])["data"])["accountSeq"] != nil {
		t.Fatal("watermark position or masking")
	}
	if page["meta"].(Object)["history_truncated"] != true {
		t.Fatal("truncation missing")
	}
	good := contract.Str(contract.Obj(page["data"])["next_cursor"])
	_, e = read(Object{"account_alias": "primary", "cursor": good + "x"})
	assertCode(t, e, "INVALID_ARGUMENT")
	_, e = read(Object{"account_alias": "secondary", "cursor": good})
	assertCode(t, e, "INVALID_ARGUMENT")
	future := s.signCursor(cursor{s.Backend, s.RunID, PrincipalID, rev, "primary", 999})
	_, e = read(Object{"account_alias": "primary", "cursor": future})
	assertCode(t, e, "INVALID_ARGUMENT")
	oldRun := s.RunID
	s.RunID = Random()
	_, e = read(Object{"account_alias": "primary", "cursor": good})
	assertCode(t, e, "CURSOR_EXPIRED")
	s.RunID = oldRun
	if e = s.Grant(context.Background(), Grant{Revision: rev, Principal: PrincipalID, Selected: []Selection{{Alias: "primary"}}}); e != nil {
		t.Fatal(e)
	}
	_, e = read(Object{"account_alias": "primary", "cursor": good})
	assertCode(t, e, "CURSOR_EXPIRED")
	rev, _ = s.Revision()
	if e = s.Grant(context.Background(), Grant{Revision: rev, Principal: PrincipalID, Selected: []Selection{}}); e != nil {
		t.Fatal(e)
	}
	_, e = read(Object{"account_alias": "primary", "cursor": good})
	assertCode(t, e, "PERMISSION_DENIED")
}
func TestQueueExactLimitsAndAge(t *testing.T) {
	now := time.Now()
	q := NewEventQueue()
	if q.MaxCount != 1024 || q.MaxBytes != 8<<20 || q.MaxWait != time.Second {
		t.Fatal("queue policy changed")
	}
	for i := 0; i < 1024; i++ {
		if !q.Push(queuedEvent{At: now, Payload: []byte("x")}, now) {
			t.Fatal(i)
		}
	}
	if q.Push(queuedEvent{At: now}, now) {
		t.Fatal("count overflow")
	}
	q = NewEventQueue()
	payload := make([]byte, 1<<20)
	for i := 0; i < 8; i++ {
		if !q.Push(queuedEvent{At: now, Payload: payload}, now) {
			t.Fatal("byte boundary")
		}
	}
	if q.Push(queuedEvent{At: now, Payload: []byte{1}}, now) {
		t.Fatal("byte overflow")
	}
	q = NewEventQueue()
	q.Push(queuedEvent{At: now}, now)
	if !q.Expired(now.Add(time.Second)) || q.Push(queuedEvent{At: now}, now.Add(time.Second)) {
		t.Fatal("age boundary")
	}
}
func TestEventHistoryPrefixAndJournalIsolation(t *testing.T) {
	s, _ := newService(t)
	grantAccounts(t, s, 2)
	now := time.Now().UTC()
	p := store.HistoryPolicy{Age: time.Hour, PerAccount: 2, Bytes: 10}
	if _, e := s.Store.DB.Exec("CREATE TABLE approval_journal(id TEXT);INSERT INTO approval_journal VALUES('preserved')"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		if e := s.Store.AppendEvent("primary", now.Add(time.Duration(i)*time.Nanosecond), []byte("abc"), p); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.Store.AppendEvent("secondary", now, []byte("12345"), p); e != nil {
		t.Fatal(e)
	}
	var count, size, watermark int64
	_ = s.Store.DB.QueryRow("SELECT count(*),coalesce(sum(size),0) FROM events").Scan(&count, &size)
	if count != 2 || size != 8 {
		t.Fatal(count, size)
	}
	_ = s.Store.DB.QueryRow("SELECT pruned_seq FROM sync WHERE alias='primary'").Scan(&watermark)
	if watermark != 3 {
		t.Fatal(watermark)
	}
	var n int
	_ = s.Store.DB.QueryRow("SELECT count(*) FROM approval_journal").Scan(&n)
	if n != 1 {
		t.Fatal("journal pruned")
	}
	// Regressing clock cannot create a hole behind a non-expired prefix.
	policy := store.HistoryPolicy{Age: time.Hour, PerAccount: 100, Bytes: 1000}
	if e := s.Store.AppendEvent("primary", now.Add(-2*time.Hour), []byte("old clock"), policy); e != nil {
		t.Fatal(e)
	}
	var remaining int
	_ = s.Store.DB.QueryRow("SELECT count(*) FROM events WHERE alias='primary'").Scan(&remaining)
	if remaining != 2 {
		t.Fatal("clock regression created hidden prefix hole")
	}

}

func TestCursorAfterDatabaseReopen(t *testing.T) {
	s, b := newService(t)
	grantAccounts(t, s, 1)
	page, e := s.Events(context.Background(), DefaultPrincipal(), Object{"account_alias": "primary"})
	if e != nil {
		t.Fatal(e)
	}
	token := contract.Str(contract.Obj(page["data"])["next_cursor"])
	var path string
	if e = s.Store.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); e != nil {
		t.Fatal(e)
	}
	s.Store.Close()
	db, e := store.Open(path, "mock")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	next, e := New(s.Catalog, b, db, "mock", "R2")
	if e != nil {
		t.Fatal(e)
	}
	_, e = next.Events(context.Background(), DefaultPrincipal(), Object{"account_alias": "primary", "cursor": token})
	assertCode(t, e, "CURSOR_EXPIRED")
}
