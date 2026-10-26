package core

import (
	"context"
	"encoding/json"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/bright-k/tossinvest-tools/internal/store"
	"github.com/bright-k/tossinvest-tools/internal/testutil"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeBroker struct {
	mu    sync.Mutex
	c     *contract.Catalog
	calls []string
	err   error
	hook  func()
	id    string
	items []any
}

func (f *fakeBroker) Identity() string { return f.id }
func (f *fakeBroker) Ready() bool      { return true }
func (f *fakeBroker) Query(ctx context.Context, op contract.Operation, args map[string]any, seq string) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, op.ID+":"+seq)
	e, hook := f.err, f.hook
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if e != nil {
		return nil, e
	}
	if op.ID == "getAccounts" {
		if f.items != nil {
			return f.items, nil
		}
		return []any{Object{"accountSeq": json.Number("1"), "accountNo": "00001111", "accountType": "BROKERAGE"}, Object{"accountSeq": json.Number("2"), "accountNo": "00002222", "accountType": "NEW_TYPE"}}, nil
	}
	v := clone(testutil.Body(f.c, op))
	if op.ID == "getOrders" || op.ID == "getConditionalOrders" {
		contract.Obj(v)["hasNext"] = false
		contract.Obj(v)["nextCursor"] = nil
	}
	if op.ID == "getOrder" {
		contract.Obj(v)["orderId"] = args["orderId"]
	}
	if op.ID == "getConditionalOrder" {
		contract.Obj(v)["conditionalOrderId"] = args["conditionalOrderId"]
	}
	return v, nil
}
func newService(t *testing.T) (*Service, *fakeBroker) {
	t.Helper()
	c := contract.Load()
	db, e := store.Open(filepath.Join(t.TempDir(), "state.sqlite3"), "mock")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	f := &fakeBroker{c: c, id: "mock-client"}
	s, e := New(c, f, db, "mock", "R1")
	if e != nil {
		t.Fatal(e)
	}
	return s, f
}
func grantAccounts(t *testing.T, s *Service, n int) {
	t.Helper()
	d, e := s.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	id := contract.Str(d["discovery_id"])
	rev, _ := s.Revision()
	selected := []Selection{}
	for i, v := range contract.List(d["accounts"]) {
		if i >= n {
			break
		}
		name := "primary"
		if i == 1 {
			name = "secondary"
		}
		selected = append(selected, Selection{contract.Str(contract.Obj(v)["handle"]), name})
	}
	if e = s.Grant(context.Background(), Grant{&id, rev, PrincipalID, selected}); e != nil {
		t.Fatal(e)
	}
}
func assertCode(t *testing.T, e error, code string) {
	t.Helper()
	if e == nil || fault.Wrap(e).Code != code {
		t.Fatalf("want %s, got %v", code, e)
	}
}
func TestAll34ServicePermissionScopeInputError(t *testing.T) {
	s, b := newService(t)
	grantAccounts(t, s, 2)
	for _, op := range s.Catalog.Ops {
		if op.Exposure != "read_tool" {
			continue
		}
		t.Run(op.ID, func(t *testing.T) {
			args := testutil.Args(s.Catalog, op, false)
			if _, e := s.Call(context.Background(), DefaultPrincipal(), op.Tool, args); e != nil {
				t.Fatal(e)
			}
			before := len(b.calls)
			p := DefaultPrincipal()
			p.Scopes = map[string]bool{}
			_, e := s.Call(context.Background(), p, op.Tool, args)
			assertCode(t, e, "PERMISSION_DENIED")
			if len(b.calls) != before {
				t.Fatal("scope denial reached upstream")
			}
			if op.Account {
				args["account_alias"] = "forbidden"
				_, e = s.Call(context.Background(), DefaultPrincipal(), op.Tool, args)
				assertCode(t, e, "PERMISSION_DENIED")
				if len(b.calls) != before {
					t.Fatal("account denial reached upstream")
				}
			}
			args = testutil.Args(s.Catalog, op, false)
			args["X-Tossinvest-Account"] = "999"
			_, e = s.Call(context.Background(), DefaultPrincipal(), op.Tool, args)
			assertCode(t, e, "INVALID_ARGUMENT")
			b.err = fault.New("UPSTREAM_ERROR", "mock failure")
			_, e = s.Call(context.Background(), DefaultPrincipal(), op.Tool, testutil.Args(s.Catalog, op, false))
			assertCode(t, e, "UPSTREAM_ERROR")
			b.err = nil
		})
	}
}
func TestEmptyAccountsAndRevocationInFlight(t *testing.T) {
	s, b := newService(t)
	d, e := s.Call(context.Background(), DefaultPrincipal(), "tossinvest_get_accounts", Object{})
	if e != nil {
		t.Fatal(e)
	}
	if len(b.calls) != 0 || d["meta"].(Object)["account_selection_required"] != true {
		t.Fatal("initial policy leaked accounts")
	}
	grantAccounts(t, s, 1)
	started, release := make(chan struct{}), make(chan struct{})
	b.hook = func() { close(started); <-release }
	done := make(chan error)
	go func() {
		_, e := s.Call(context.Background(), DefaultPrincipal(), "tossinvest_get_holdings", Object{"account_alias": "primary"})
		done <- e
	}()
	<-started
	rev, _ := s.Revision()
	b.err = fault.New("UPSTREAM_ERROR", "offline")
	if e = s.Grant(context.Background(), Grant{Revision: rev, Principal: PrincipalID, Selected: []Selection{}}); e != nil {
		t.Fatal(e)
	}
	close(release)
	assertCode(t, <-done, "PERMISSION_DENIED")
	b.hook = nil
}
func TestGrantExpiryConflictUnknownPrincipalAliasAndClient(t *testing.T) {
	s, _ := newService(t)
	now := time.Now()
	s.Now = func() time.Time { return now }
	d, e := s.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	id := contract.Str(d["discovery_id"])
	rev, _ := s.Revision()
	sel := []Selection{{contract.Str(contract.Obj(contract.List(d["accounts"])[0])["handle"]), "primary"}}
	now = now.Add(60 * time.Second)
	assertCode(t, s.Grant(context.Background(), Grant{&id, rev, PrincipalID, sel}), "CONFIG_CONFLICT")
	now = now.Add(-time.Minute)
	assertCode(t, s.Grant(context.Background(), Grant{&id, rev, "unknown", sel}), "INVALID_ARGUMENT")
	assertCode(t, s.Grant(context.Background(), Grant{&id, rev + 1, PrincipalID, sel}), "CONFIG_CONFLICT")
	grantAccounts(t, s, 2)
	s.Broker.(*fakeBroker).id = "different-client"
	_, e = s.Call(context.Background(), DefaultPrincipal(), "tossinvest_get_holdings", Object{"account_alias": "primary"})
	assertCode(t, e, "PERMISSION_DENIED")
}
func TestUnknownIDsAccountBoundNoFallback(t *testing.T) {
	s, b := newService(t)
	grantAccounts(t, s, 2)
	for _, name := range []string{"getOrder", "getConditionalOrder"} {
		op, _ := s.Catalog.Find(name)
		args := testutil.Args(s.Catalog, op, false)
		before := len(b.calls)
		_, e := s.Call(context.Background(), DefaultPrincipal(), op.Tool, args)
		if e != nil {
			t.Fatal(e)
		}
		if len(b.calls) != before+1 || b.calls[len(b.calls)-1] != name+":1" {
			t.Fatal("unknown ID fallback")
		}
		args["account_alias"] = "secondary"
		before = len(b.calls)
		_, e = s.Call(context.Background(), DefaultPrincipal(), op.Tool, args)
		assertCode(t, e, "PERMISSION_DENIED")
		if len(b.calls) != before {
			t.Fatal("confirmed other account transmitted")
		}
		for _, code := range []string{"RESOURCE_NOT_FOUND", "UPSTREAM_RATE_LIMITED", "UPSTREAM_ERROR"} {
			args = testutil.Args(s.Catalog, op, false)
			key := "orderId"
			if name == "getConditionalOrder" {
				key = "conditionalOrderId"
			}
			args[key] = "new-" + code
			b.err = fault.New(code, "mock")
			before = len(b.calls)
			_, e = s.Call(context.Background(), DefaultPrincipal(), op.Tool, args)
			assertCode(t, e, code)
			if len(b.calls) != before+1 {
				t.Fatal("failure retried across accounts")
			}
			kind, id := resourceID(op, args)
			var n int
			_ = s.Store.DB.QueryRow("SELECT count(*) FROM relations WHERE kind=? AND id=?", kind, id).Scan(&n)
			if n != 0 {
				t.Fatal("failure inferred ownership")
			}
			b.err = nil
		}
	}
	if e := s.record("order", "collision", "primary"); e != nil {
		t.Fatal(e)
	}
	assertCode(t, s.record("order", "collision", "secondary"), "UPSTREAM_DATA_INVALID")
}
func TestReadonlyCoreAndLegacyJournal(t *testing.T) {
	s, b := newService(t)
	_, e := s.Store.DB.Exec("CREATE TABLE legacy_dispatch(id TEXT,state TEXT);INSERT INTO legacy_dispatch VALUES('pending','dispatching')")
	if e != nil {
		t.Fatal(e)
	}
	for _, op := range s.Catalog.Ops {
		if op.Exposure != "approved_action" {
			continue
		}
		_, e = s.Call(context.Background(), DefaultPrincipal(), op.Tool, Object{"approved": true, "confirm": true})
		assertCode(t, e, "READONLY_VIOLATION")
	}
	if len(b.calls) != 0 {
		t.Fatal("legacy writes dispatched")
	}
	status, e := s.Status(DefaultPrincipal())
	if e != nil {
		t.Fatal(e)
	}
	if status["trading_ready"] != false || status["query_ready"] != true {
		t.Fatal("readonly readiness")
	}
}
func TestInputBoundaries(t *testing.T) {
	s, _ := newService(t)
	for _, op := range s.Catalog.Ops {
		if op.Method != "GET" {
			continue
		}
		for _, param := range s.Catalog.Params(op) {
			if param["in"] == "header" {
				continue
			}
			name := contract.Str(param["name"])
			schema := s.Catalog.Resolve(contract.Obj(param["schema"]))
			args := testutil.Args(s.Catalog, op, false)
			if param["required"] == true {
				delete(args, name)
				if s.Catalog.Input(op, args) == nil {
					t.Fatalf("%s missing %s", op.ID, name)
				}
			}
			args = testutil.Args(s.Catalog, op, false)
			if schema["enum"] != nil {
				args[name] = "unknown-request-enum"
				if s.Catalog.Input(op, args) == nil {
					t.Fatalf("%s enum", op.ID)
				}
			}
			if schema["maximum"] != nil {
				args[name] = json.Number("999999")
				if s.Catalog.Input(op, args) == nil {
					t.Fatalf("%s maximum", op.ID)
				}
			}
		}
	}
	op, _ := s.Catalog.Find("getPrices")
	if s.Catalog.Input(op, Object{"symbols": strings.Repeat("A,", 200) + "A"}) == nil {
		t.Fatal("symbols cap")
	}
	op, _ = s.Catalog.Find("getOrders")
	if s.Catalog.Input(op, Object{"account_alias": "primary", "status": "OPEN", "from": "2026-03-02", "to": "2026-03-01"}) == nil {
		t.Fatal("date order")
	}

}

func TestDiscoveryDisappearanceAndAliasTombstones(t *testing.T) {
	s, b := newService(t)
	d, e := s.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	id := contract.Str(d["discovery_id"])
	rev, _ := s.Revision()
	selected := []Selection{{contract.Str(contract.Obj(contract.List(d["accounts"])[0])["handle"]), "primary"}}
	b.items = []any{}
	assertCode(t, s.Grant(context.Background(), Grant{&id, rev, PrincipalID, selected}), "CONFIG_CONFLICT")
	b.items = nil
	grantAccounts(t, s, 1)
	d, e = s.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	id = contract.Str(d["discovery_id"])
	rev, _ = s.Revision()
	selected = []Selection{{contract.Str(contract.Obj(contract.List(d["accounts"])[0])["handle"]), "renamed"}}
	assertCode(t, s.Grant(context.Background(), Grant{&id, rev, PrincipalID, selected}), "INVALID_ARGUMENT")
	if e = s.Grant(context.Background(), Grant{Revision: rev, Principal: PrincipalID, Selected: []Selection{}}); e != nil {
		t.Fatal(e)
	}
	d, e = s.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	id = contract.Str(d["discovery_id"])
	rev, _ = s.Revision()
	selected = []Selection{{contract.Str(contract.Obj(contract.List(d["accounts"])[0])["handle"]), "renamed"}}
	if e = s.Grant(context.Background(), Grant{&id, rev, PrincipalID, selected}); e != nil {
		t.Fatal(e)
	}
	d, e = s.Discover(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	id = contract.Str(d["discovery_id"])
	rev, _ = s.Revision()
	selected = []Selection{{contract.Str(contract.Obj(contract.List(d["accounts"])[1])["handle"]), "primary"}}
	assertCode(t, s.Grant(context.Background(), Grant{&id, rev, PrincipalID, selected}), "INVALID_ARGUMENT")
}

func TestResultMetadataAndStatusOnlyScope(t *testing.T) {
	s, _ := newService(t)
	for _, name := range []string{"tossinvest_get_server_status", "tossinvest_get_prices", "tossinvest_get_accounts"} {
		args := Object{}
		if name == "tossinvest_get_prices" {
			args["symbols"] = "AAPL"
		}
		result, e := s.Call(context.Background(), DefaultPrincipal(), name, args)
		if e != nil {
			t.Fatal(e)
		}
		m := contract.Obj(result["meta"])
		for _, key := range []string{"schema_version", "mode", "backend", "fetched_at", "data_timestamp", "stale"} {
			if _, ok := m[key]; !ok {
				t.Fatal("missing meta", key)
			}
		}
		if m["mode"] != "readonly" || m["backend"] != "mock" || m["data_timestamp"] != nil || m["stale"] != nil {
			t.Fatal("metadata assumptions")
		}
	}
	p := Principal{ID: PrincipalID, Scopes: map[string]bool{"status:read": true}}
	if _, e := s.Status(p); e != nil {
		t.Fatal("status-only principal denied", e)
	}
}
