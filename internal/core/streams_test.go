package core

import (
	"context"
	"encoding/json"
	"github.com/bright-k/tossinvest-tools/internal/broker/mock"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func eventually(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	until := time.Now().Add(timeout)
	for time.Now().Before(until) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func eventFrame(c *contract.Catalog, seq string) Object {
	ch := contract.Obj(contract.Obj(c.Async["channels"])["realtime-order"])
	m := contract.Obj(contract.Obj(ch["messages"])["orderStream"])
	frame := contract.Obj(clone(contract.Obj(contract.List(m["examples"])[0])["payload"]))
	frame["topic"] = "personal:order:" + seq
	contract.Obj(frame["data"])["accountSeq"] = seq
	contract.Obj(contract.Obj(frame["data"])["order"])["orderId"] = "event-" + seq
	return frame
}
func TestWSAllMessagesVariantsAckAndDeclarationErrors(t *testing.T) {
	s, _ := newService(t)
	grantAccounts(t, s, 1)
	x := NewStreams(s, StreamOptions{})
	s.Streams = x
	x.generation = 1
	x.connected = true
	x.aliases = map[string]string{"1": "primary"}
	x.active = map[string]bool{"personal:order:1": true, "trade:kr:005930": true, "trade:us:AAPL": true, "orderbook:kr:005930": true, "orderbook:us:AAPL": true}
	channels := contract.Obj(s.Catalog.Async["channels"])
	n := 0
	for _, ch := range []string{"realtime-trade", "realtime-orderbook"} {
		messages := contract.Obj(contract.Obj(channels[ch])["messages"])
		key := "tradeStream"
		kind := "trade"
		if ch == "realtime-orderbook" {
			key = "orderbookStream"
			kind = "orderbook"
		}
		for _, v := range contract.List(contract.Obj(messages[key])["examples"]) {
			for _, market := range []string{"kr", "us"} {
				frame := contract.Obj(clone(contract.Obj(v)["payload"]))
				code := "AAPL"
				if market == "kr" {
					code = "005930"
				}
				frame["topic"] = kind + ":" + market + ":" + code
				if e := x.process(context.Background(), mustJSON(frame)); e != nil {
					t.Fatal(e)
				}
				n++
			}
		}
	}
	frame := eventFrame(s.Catalog, "1")
	schema := contract.Obj(contract.Obj(contract.Obj(contract.Obj(channels["realtime-order"])["messages"])["orderStream"])["payload"])
	dataSchema := contract.Obj(contract.Obj(schema["properties"])["data"])
	events := contract.List(contract.Obj(contract.Obj(dataSchema["properties"])["event"])["enum"])
	events = append(events, "FUTURE_EVENT")
	for _, event := range events {
		contract.Obj(frame["data"])["event"] = event
		if e := x.process(context.Background(), mustJSON(frame)); e != nil {
			t.Fatal(e)
		}
		n++
	}
	if n < 15 {
		t.Fatal("WS variants missing")
	}
	orderSchema := contract.Obj(contract.Obj(dataSchema["properties"])["order"])
	statuses := contract.List(contract.Obj(contract.Obj(orderSchema["properties"])["status"])["enum"])
	statuses = append(statuses, "FUTURE_STATUS")
	for _, status := range statuses {
		contract.Obj(contract.Obj(frame["data"])["order"])["status"] = status
		if e := x.process(context.Background(), mustJSON(frame)); e != nil {
			t.Fatal("order status variant", e)
		}
	}

	// A whole-declaration error keeps the acknowledged set. An unrelated echoed id cannot replace it.
	x.pendingID = "pending"
	x.pending = map[string]bool{"trade:us:AAPL": true, "trade:us:NOPE": true}
	if e := x.process(context.Background(), []byte(`{"type":"error","id":"pending","error":{"code":"too-many-topics","message":"limit"}}`)); e != nil {
		t.Fatal(e)
	}
	if !x.active["personal:order:1"] {
		t.Fatal("whole-declaration error cleared active set")
	}
	x.pendingID = "pending"
	if e := x.process(context.Background(), []byte(`{"type":"subscriptions","id":"stale","subscribed":[],"rejected":[]}`)); e != nil {
		t.Fatal(e)
	}
	if x.pendingID != "pending" {
		t.Fatal("stale ack applied")
	}
	// Cancel sync's context so this focused ACK test does not start background network work.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := x.process(ctx, []byte(`{"type":"subscriptions","id":"pending","subscribed":["trade:us:AAPL"],"rejected":[{"target":"trade:us:NOPE","code":"stock-not-found","message":"no"}]}`)); e != nil {
		t.Fatal(e)
	}
	x.wg.Wait()
	if !x.active["trade:us:AAPL"] || x.active["trade:us:NOPE"] || x.blocked["trade:us:NOPE"] != "stock-not-found" {
		t.Fatal("partial rejection")
	}
	desired, _, e := x.desired()
	if e != nil {
		t.Fatal(e)
	}
	if desired["trade:us:NOPE"] {
		t.Fatal("rejected topic retried")
	}
	if x.process(context.Background(), []byte(`{"type":"error","error":{"code":"server-shutdown","message":"shutdown"}}`)) == nil {
		t.Fatal("shutdown not reconnecting")
	}
	if x.process(context.Background(), []byte(`{"type":"message","topic":"personal:order:1","data":{}}`)) == nil {
		t.Fatal("corrupt event accepted")
	}
	frame = eventFrame(s.Catalog, "1")
	contract.Obj(frame["data"])["accountSeq"] = "2"
	x.active["personal:order:1"] = true
	if x.process(context.Background(), mustJSON(frame)) == nil {
		t.Fatal("account mismatch accepted")
	}
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func TestWSReconnectHeartbeatStorageFailureAndRevocation(t *testing.T) {
	s, _ := newService(t)
	m := mock.New(s.Catalog)
	defer m.Close()
	if _, e := m.REST.Token(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
	s.Broker = m.REST
	grantAccounts(t, s, 1)
	x := NewStreams(s, StreamOptions{URL: m.WSURL(), Token: m.REST.Token, Ping: 30 * time.Millisecond, Backoff: 10 * time.Millisecond})
	s.Streams = x
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.Start(ctx)
	defer x.Close()
	eventually(t, 8*time.Second, func() bool { return x.ReadyLocked() })
	eventually(t, time.Second, func() bool { x.mu.Lock(); defer x.mu.Unlock(); return !x.lastPong.IsZero() })
	initial := m.ConnectionCount()
	m.Send(Object{"type": "error", "error": Object{"code": "server-shutdown", "message": "mock deploy"}})
	eventually(t, 8*time.Second, func() bool { return m.ConnectionCount() > initial && x.ReadyLocked() })
	page, e := s.Events(ctx, DefaultPrincipal(), Object{"account_alias": "primary"})
	if e != nil {
		t.Fatal(e)
	}
	if page["meta"].(Object)["gap_detected"] != true || page["meta"].(Object)["sync_status"] != "synced" {
		t.Fatal("recovery erased observed gap")
	}
	events := contract.List(contract.Obj(page["data"])["events"])
	if len(events) < 2 {
		t.Fatal("reconnect event history not preserved")
	}
	// DB write failure changes memory readiness immediately; reader/heartbeat do not wait for DB repair.
	s.Mu.Lock()
	_, e = s.Store.DB.Exec("PRAGMA query_only=ON")
	s.Mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	m.Send(eventFrame(s.Catalog, "1"))
	eventually(t, 2*time.Second, func() bool { x.mu.Lock(); defer x.mu.Unlock(); return x.dbErr && x.gap })
	if x.ReadyLocked() {
		t.Fatal("storage failure ready")
	}
	s.Mu.Lock()
	_, e = s.Store.DB.Exec("PRAGMA query_only=OFF")
	s.Mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	eventually(t, 10*time.Second, func() bool { return x.ReadyLocked() })
	rev, _ := s.Revision()
	if e = s.Grant(ctx, Grant{Revision: rev, Principal: PrincipalID, Selected: []Selection{}}); e != nil {
		t.Fatal(e)
	}
	s.Mu.Lock()
	status := x.StatusLocked(DefaultPrincipal())
	s.Mu.Unlock()
	b := string(mustJSON(status))
	if strings.Contains(b, "primary") || strings.Contains(b, "personal:order:1") {
		t.Fatal("revoked account leaked from stream status")
	}
	_, e = s.Events(ctx, DefaultPrincipal(), Object{"account_alias": "primary"})
	assertCode(t, e, "PERMISSION_DENIED")
}
func TestSlowWriterQueueOverflowDoesNotBlockReader(t *testing.T) {
	s, _ := newService(t)
	grantAccounts(t, s, 1)
	x := NewStreams(s, StreamOptions{})
	x.queue.MaxCount = 1
	x.connected = true
	x.generation = 1
	x.active["personal:order:1"] = true
	x.aliases["1"] = "primary"
	frame := mustJSON(eventFrame(s.Catalog, "1"))
	if e := x.process(context.Background(), frame); e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	if e := x.process(context.Background(), frame); e == nil {
		t.Fatal("overflow did not fail connection")
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("reader blocked on writer")
	}
	if !x.gap {
		t.Fatal("overflow not marked")
	}
	if e := x.process(context.Background(), frame); e == nil || !x.gap {
		t.Fatal("repeated saturation erased gap or accepted overflow")
	}
	x.dbErr = true
	if x.ReadyLocked() {
		t.Fatal("overflow reports ready")
	}

}

func TestActualWSFrameLimitAndQueuedDispatchRevocation(t *testing.T) {
	s, _ := newService(t)
	m := mock.New(s.Catalog)
	defer m.Close()
	if _, e := m.REST.Token(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
	s.Broker = m.REST
	grantAccounts(t, s, 1)
	// Occupy the ASSET rate slot. The next service call must check current permission again after pacing.
	op, _ := s.Catalog.Find("getHoldings")
	if _, e := m.REST.Query(context.Background(), op, Object{"account_alias": "primary"}, "1"); e != nil {
		t.Fatal(e)
	}
	before := m.CallCount("getHoldings")
	done := make(chan error)
	go func() {
		_, e := s.Call(context.Background(), DefaultPrincipal(), op.Tool, Object{"account_alias": "primary"})
		done <- e
	}()
	time.Sleep(30 * time.Millisecond)
	rev, _ := s.Revision()
	if e := s.Grant(context.Background(), Grant{Revision: rev, Principal: PrincipalID, Selected: []Selection{}}); e != nil {
		t.Fatal(e)
	}
	assertCode(t, <-done, "PERMISSION_DENIED")
	if m.CallCount("getHoldings") != before {
		t.Fatal("revoked queued read transmitted")
	}
	grantAccounts(t, s, 1)
	x := NewStreams(s, StreamOptions{URL: m.WSURL(), Token: m.REST.Token, Backoff: 10 * time.Millisecond})
	s.Streams = x
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.Start(ctx)
	defer x.Close()
	eventually(t, 10*time.Second, func() bool { return x.ReadyLocked() })
	base := eventFrame(s.Catalog, "1")
	base["padding"] = ""
	b := mustJSON(base)
	base["padding"] = strings.Repeat("x", (1<<20)-len(b))
	exact := mustJSON(base)
	if len(exact) != 1<<20 {
		t.Fatal("test frame boundary")
	}
	beforeEvents := int64(0)
	s.Mu.Lock()
	_ = s.Store.DB.QueryRow("SELECT count(*) FROM events").Scan(&beforeEvents)
	s.Mu.Unlock()
	m.SendRaw(exact)
	eventually(t, 2*time.Second, func() bool {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		var n int64
		_ = s.Store.DB.QueryRow("SELECT count(*) FROM events").Scan(&n)
		return n > beforeEvents
	})
	initial := m.ConnectionCount()
	base["padding"] = strings.Repeat("x", (1<<20)-len(b)+1)
	m.SendRaw(mustJSON(base))
	eventually(t, 10*time.Second, func() bool { return m.ConnectionCount() > initial && x.ReadyLocked() })
}

func TestRecoveryOverlapCannotPublishOlderSnapshot(t *testing.T) {
	s, b := newService(t)
	grantAccounts(t, s, 1)
	x := NewStreams(s, StreamOptions{})
	x.connected = true
	x.generation = 1
	x.active["personal:order:1"] = true
	x.aliases["1"] = "primary"
	b.hook = func() {
		x.mu.Lock()
		x.versions["primary"]++
		x.committed["primary"] = x.versions["primary"]
		x.mu.Unlock()
	}
	if x.synchronize(context.Background()) == nil {
		t.Fatal("changing snapshot reported synced")
	}
	if x.synced["primary"] {
		t.Fatal("overlap moved state backward")
	}
	b.hook = nil
	if e := x.synchronize(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !x.synced["primary"] {
		t.Fatal("stable recovery not published")
	}
	var n int
	_ = s.Store.DB.QueryRow("SELECT count(*) FROM snapshots WHERE alias='primary'").Scan(&n)
	if n != 1 {
		t.Fatal("snapshot not persisted")
	}
}

func TestRecoveryStopsNewReadsAfterGrantRevocation(t *testing.T) {
	s, _ := newService(t)
	m := mock.New(s.Catalog)
	defer m.Close()
	if _, err := m.REST.Token(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	s.Broker = m.REST
	grantAccounts(t, s, 1)
	x := NewStreams(s, StreamOptions{})
	x.connected = true
	x.generation = 1
	x.active["personal:order:1"] = true
	x.aliases["1"] = "primary"
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.Hook = func(r *http.Request) {
		if r.URL.Path == "/api/v1/holdings" {
			once.Do(func() { close(started); <-release })
		}
	}
	done := make(chan error, 1)
	go func() { done <- x.synchronize(context.Background()) }()
	<-started
	revision, err := s.Revision()
	if err != nil {
		t.Fatal(err)
	}
	err = s.Grant(context.Background(), Grant{Revision: revision, Principal: PrincipalID, Selected: []Selection{}})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	assertCode(t, <-done, "PERMISSION_DENIED")
	if m.CallCount("getOrders") != 0 || m.CallCount("getConditionalOrders") != 0 {
		t.Fatal("revoked recovery sent subsequent account requests")
	}
	var snapshots int
	_ = s.Store.DB.QueryRow("SELECT count(*) FROM snapshots").Scan(&snapshots)
	if snapshots != 0 {
		t.Fatal("revoked recovery published a snapshot")
	}
}

func TestStreamSnapshotFreshnessIsUnknownWhileConnected(t *testing.T) {
	s, _ := newService(t)
	x := NewStreams(s, StreamOptions{})
	x.connected = true
	x.watch["trade:us:AAPL"] = true
	x.snapshots["trade:us:AAPL"] = Object{"topic": "trade:us:AAPL", "data": Object{"timestamp": "2026-01-01T00:00:00Z", "price": "1.00"}}
	args := Object{"channel": "trade:us", "symbol": "AAPL"}
	result, handled, err := x.Call(context.Background(), DefaultPrincipal(), "tossinvest_get_stream_snapshot", args)
	if err != nil || !handled {
		t.Fatal(err)
	}
	meta := contract.Obj(result["meta"])
	if meta["stale"] != nil || meta["data_timestamp"] != "2026-01-01T00:00:00Z" {
		t.Fatal("connection status asserted price freshness")
	}
	x.connected = false
	result, _, err = x.Call(context.Background(), DefaultPrincipal(), "tossinvest_get_stream_snapshot", args)
	if err != nil || contract.Obj(result["meta"])["stale"] != true {
		t.Fatal("disconnected snapshot not marked stale")
	}
}
