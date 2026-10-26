package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/bright-k/tossinvest-tools/internal/store"
)

type Object = map[string]any

const PrincipalID = "codex-readonly"

type Principal struct {
	ID     string
	Scopes map[string]bool
}

func DefaultPrincipal() Principal {
	return Principal{ID: PrincipalID, Scopes: map[string]bool{"status:read": true, "market:read": true, "accounts:read": true, "streams:read": true, "streams:manage": true}}
}
func (p Principal) Require(scope string) error {
	if p.ID != PrincipalID || !p.Scopes[scope] {
		return fault.New("PERMISSION_DENIED", "scope denied")
	}
	return nil
}

type Account struct {
	Alias  string `json:"account_alias"`
	Kind   string `json:"account_type"`
	Masked string `json:"account_no_masked"`
	Seq    string `json:"-"`
}
type discovered struct{ Seq, Kind, Last4 string }
type Discovery struct {
	Expires  time.Time
	Revision int64
	Items    map[string]discovered
}
type StreamController interface {
	Notify()
	StatusLocked(Principal) Object
	ReadyLocked() bool
	Call(context.Context, Principal, string, Object) (Object, bool, error)
}
type Service struct {
	Catalog                   *contract.Catalog
	Broker                    toss.Broker
	Store                     *store.Store
	Backend, RunID, Milestone string
	Mu                        sync.Mutex
	Discoveries               map[string]Discovery
	Streams                   StreamController
	Now                       func() time.Time
	cursorKey                 []byte
}

func Random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func New(c *contract.Catalog, b toss.Broker, db *store.Store, backend, milestone string) (*Service, error) {
	s := &Service{Catalog: c, Broker: b, Store: db, Backend: backend, Milestone: milestone, RunID: Random(), Discoveries: map[string]Discovery{}, Now: time.Now, cursorKey: []byte(Random())}
	var key string
	if err := db.DB.QueryRow("SELECT value FROM metadata WHERE key='cursor_key'").Scan(&key); err != nil {
		key = Random()
		if _, err = db.DB.Exec("INSERT INTO metadata VALUES('cursor_key',?)", key); err != nil {
			return nil, err
		}
	}
	s.cursorKey = []byte(key)
	if b.Identity() != "" {
		if e := s.bindClient(); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func (s *Service) revision() (int64, error) {
	var n int64
	err := s.Store.DB.QueryRow("SELECT CAST(value AS INTEGER) FROM metadata WHERE key='revision'").Scan(&n)
	return n, err
}
func (s *Service) Revision() (int64, error) { s.Mu.Lock(); defer s.Mu.Unlock(); return s.revision() }
func (s *Service) bindClient() error {
	id := s.Broker.Identity()
	if id == "" {
		return fault.New("NOT_READY", "client identity unavailable")
	}
	var old string
	err := s.Store.DB.QueryRow("SELECT value FROM metadata WHERE key='client'").Scan(&old)
	if err == nil && old == id {
		return nil
	}
	tx, err := s.Store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM grants; DELETE FROM relations; DELETE FROM watchlist; UPDATE accounts SET active=0; UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='revision'"); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO metadata VALUES('client',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) allowed(p Principal, alias string) (Account, error) {
	if err := p.Require("accounts:read"); err != nil {
		return Account{}, err
	}
	if err := s.bindClient(); err != nil {
		return Account{}, err
	}
	a := Account{}
	var last string
	err := s.Store.DB.QueryRow(`SELECT a.alias,a.seq,a.kind,a.last4 FROM accounts a JOIN grants g ON g.alias=a.alias WHERE g.principal=? AND a.alias=? AND active=1 AND client=?`, p.ID, alias, s.Broker.Identity()).Scan(&a.Alias, &a.Seq, &a.Kind, &last)
	if err != nil {
		return Account{}, fault.New("PERMISSION_DENIED", "account denied; select accounts locally")
	}
	a.Masked = "****" + last
	return a, nil
}
func (s *Service) accounts(p Principal) ([]Account, error) {
	if err := p.Require("accounts:read"); err != nil {
		return nil, err
	}
	if err := s.bindClient(); err != nil {
		return nil, err
	}
	rows, err := s.Store.DB.Query(`SELECT a.alias,a.seq,a.kind,a.last4 FROM accounts a JOIN grants g ON g.alias=a.alias WHERE g.principal=? AND active=1 AND client=? ORDER BY a.alias`, p.ID, s.Broker.Identity())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a := Account{}
		var last string
		if err := rows.Scan(&a.Alias, &a.Seq, &a.Kind, &last); err != nil {
			return nil, err
		}
		a.Masked = "****" + last
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Service) Discover(ctx context.Context) (Object, error) {
	op, _ := s.Catalog.Find("getAccounts")
	data, err := s.Broker.Query(ctx, op, Object{}, "")
	if err != nil {
		return nil, err
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if err = s.bindClient(); err != nil {
		return nil, err
	}
	rev, err := s.revision()
	if err != nil {
		return nil, err
	}
	d := Discovery{Expires: s.Now().Add(60 * time.Second), Revision: rev, Items: map[string]discovered{}}
	items := []any{}
	for i, v := range contract.List(data) {
		m := contract.Obj(v)
		seq := fmt.Sprint(m["accountSeq"])
		no := contract.Str(m["accountNo"])
		if len(no) < 4 {
			return nil, fault.New("UPSTREAM_DATA_INVALID", "account number invalid")
		}
		handle := Random()
		d.Items[handle] = discovered{seq, contract.Str(m["accountType"]), no[len(no)-4:]}
		items = append(items, Object{"handle": handle, "account_type": m["accountType"], "account_no_masked": "****" + no[len(no)-4:], "suggested_alias": fmt.Sprintf("account-%d", i+1)})
	}
	for k, v := range s.Discoveries {
		if !s.Now().Before(v.Expires) {
			delete(s.Discoveries, k)
		}
	}
	id := Random()
	s.Discoveries[id] = d
	return Object{"discovery_id": id, "expected_access_revision": rev, "principal_id": PrincipalID, "accounts": items, "expires_at": d.Expires}, nil
}

type Selection struct {
	Handle string `json:"handle"`
	Alias  string `json:"alias"`
}
type Grant struct {
	DiscoveryID *string     `json:"discovery_id"`
	Revision    int64       `json:"expected_access_revision"`
	Principal   string      `json:"principal_id"`
	Selected    []Selection `json:"selected"`
}

func (s *Service) Grant(ctx context.Context, g Grant) error {
	if g.Principal != PrincipalID || len(g.Selected) > 100 {
		return fault.New("INVALID_ARGUMENT", "invalid principal or selection")
	}
	s.Mu.Lock()
	rev, err := s.revision()
	if err != nil {
		s.Mu.Unlock()
		return err
	}
	if rev != g.Revision {
		s.Mu.Unlock()
		return fault.New("CONFIG_CONFLICT", "access revision changed")
	}
	d := Discovery{}
	if g.DiscoveryID != nil {
		d = s.Discoveries[*g.DiscoveryID]
		if !s.Now().Before(d.Expires) || d.Revision != rev {
			s.Mu.Unlock()
			return fault.New("CONFIG_CONFLICT", "discovery expired")
		}
	}
	selected := map[string]discovered{}
	seqs := map[string]bool{}
	for _, sel := range g.Selected {
		if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`).MatchString(sel.Alias) {
			s.Mu.Unlock()
			return fault.New("INVALID_ARGUMENT", "invalid alias")
		}
		a, ok := d.Items[sel.Handle]
		if g.DiscoveryID == nil {
			existing, e := s.allowed(DefaultPrincipal(), sel.Alias)
			if e != nil {
				s.Mu.Unlock()
				return e
			}
			a = discovered{existing.Seq, existing.Kind, strings.TrimPrefix(existing.Masked, "****")}
			ok = true
		}
		if !ok || seqs[a.Seq] || selected[sel.Alias].Seq != "" {
			s.Mu.Unlock()
			return fault.New("INVALID_ARGUMENT", "invalid or duplicate handle")
		}
		selected[sel.Alias] = a
		seqs[a.Seq] = true
	}
	s.Mu.Unlock()
	if g.DiscoveryID != nil && len(selected) > 0 {
		op, _ := s.Catalog.Find("getAccounts")
		data, e := s.Broker.Query(ctx, op, Object{}, "")
		if e != nil {
			return e
		}
		present := map[string]bool{}
		for _, v := range contract.List(data) {
			present[fmt.Sprint(contract.Obj(v)["accountSeq"])] = true
		}
		for _, a := range selected {
			if !present[a.Seq] {
				return fault.New("CONFIG_CONFLICT", "account disappeared; rediscover")
			}
		}
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if err = s.bindClient(); err != nil {
		return err
	}
	rev, err = s.revision()
	if err != nil {
		return err
	}
	if rev != g.Revision || (g.DiscoveryID != nil && !s.Now().Before(d.Expires)) {
		return fault.New("CONFIG_CONFLICT", "selection changed or expired")
	}
	for alias, a := range selected {
		var oldAlias string
		_ = s.Store.DB.QueryRow("SELECT alias FROM accounts WHERE seq=? AND client=? AND active=1", a.Seq, s.Broker.Identity()).Scan(&oldAlias)
		if oldAlias != "" && oldAlias != alias {
			var n int
			if err = s.Store.DB.QueryRow("SELECT count(*) FROM grants WHERE alias=?", oldAlias).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return fault.New("INVALID_ARGUMENT", "revoke existing alias before renaming")
			}
		}
	}
	tx, err := s.Store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM grants WHERE principal=?", g.Principal); err != nil {
		return err
	}
	for alias, a := range selected {
		var oldSeq, oldAlias, oldClient string
		_ = tx.QueryRow("SELECT seq,client FROM accounts WHERE alias=?", alias).Scan(&oldSeq, &oldClient)
		_ = tx.QueryRow("SELECT alias FROM accounts WHERE seq=? AND client=? AND active=1", a.Seq, s.Broker.Identity()).Scan(&oldAlias)
		if oldSeq != "" && (oldSeq != a.Seq || oldClient != s.Broker.Identity()) {
			return fault.New("INVALID_ARGUMENT", "alias cannot be reassigned")
		}
		if oldAlias != "" && oldAlias != alias {
			var n int
			_ = tx.QueryRow("SELECT count(*) FROM grants WHERE alias=?", oldAlias).Scan(&n)
			if n > 0 {
				return fault.New("INVALID_ARGUMENT", "revoke existing alias first")
			}
			if _, err = tx.Exec("UPDATE accounts SET active=0 WHERE alias=?", oldAlias); err != nil {
				return err
			}
		}
		if _, err = tx.Exec("INSERT INTO accounts(alias,seq,kind,last4,client,active) VALUES(?,?,?,?,?,1) ON CONFLICT(alias) DO UPDATE SET kind=excluded.kind,last4=excluded.last4,client=excluded.client,active=1", alias, a.Seq, a.Kind, a.Last4, s.Broker.Identity()); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO grants VALUES(?,?)", g.Principal, alias); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT OR IGNORE INTO sync(alias) VALUES(?)", alias); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='revision'"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.Discoveries = map[string]Discovery{}
	if s.Streams != nil {
		s.Streams.Notify()
	}
	return nil
}
func (s *Service) meta() Object {
	return Object{"schema_version": 1, "mode": "readonly", "backend": s.Backend, "fetched_at": s.Now().UTC().Format(time.RFC3339Nano), "data_timestamp": nil, "stale": nil}
}
func (s *Service) Status(p Principal) (Object, error) {
	if err := p.Require("status:read"); err != nil {
		return nil, err
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	a := []Account{}
	var err error
	if p.Scopes["accounts:read"] {
		a, err = s.accounts(p)
	}
	if err != nil && s.Broker.Ready() {
		return nil, err
	}
	aliases := []string{}
	for _, x := range a {
		aliases = append(aliases, x.Alias)
	}
	streamReady := false
	var stream any
	cap := []string{"rest_read", "mcp", "cli", "local_accounts"}
	if s.Milestone == "R2" {
		cap = append(cap, "streams")
		if s.Streams != nil {
			stream = s.Streams.StatusLocked(p)
			streamReady = s.Streams.ReadyLocked()
		}
	}
	reasons := []string{}
	if !s.Broker.Ready() {
		reasons = append(reasons, "TOSS_CONNECTION_NOT_READY")
	}
	if len(a) == 0 {
		reasons = append(reasons, "ACCOUNT_SELECTION_REQUIRED")
	}
	return Object{"mode": "readonly", "backend": s.Backend, "milestone": s.Milestone, "query_ready": s.Broker.Ready(), "streams_ready": streamReady, "trading_ready": false, "implemented_capabilities": cap, "active_capabilities": cap, "account_aliases": aliases, "limitations": reasons, "stream": stream, "run_id": s.RunID, "registered_service_mode": nil}, nil
}
func (s *Service) Call(ctx context.Context, p Principal, tool string, args Object) (Object, error) {
	if tool == "tossinvest_get_server_status" {
		if len(args) != 0 {
			return nil, fault.New("INVALID_ARGUMENT", "status has no arguments")
		}
		d, e := s.Status(p)
		return Object{"data": d, "meta": s.meta()}, e
	}
	if strings.HasPrefix(tool, "tossinvest_prepare_") || tool == "tossinvest_execute_approved_action" || tool == "tossinvest_pause_writes" {
		return nil, fault.New("READONLY_VIOLATION", "trading module unavailable")
	}
	if s.Streams != nil && s.Milestone == "R2" {
		if result, handled, err := s.Streams.Call(ctx, p, tool, args); handled {
			return result, err
		}
	}
	op, ok := s.Catalog.Find(tool)
	if !ok || op.Method != "GET" || op.Exposure != "read_tool" {
		return nil, fault.New("READONLY_VIOLATION", "unregistered readonly operation")
	}
	if err := s.Catalog.Input(op, args); err != nil {
		return nil, fault.New("INVALID_ARGUMENT", err.Error())
	}
	scope := "market:read"
	if op.Account || op.ID == "getAccounts" {
		scope = "accounts:read"
	}
	if err := p.Require(scope); err != nil {
		return nil, err
	}
	s.Mu.Lock()
	rev, err := s.revision()
	if err != nil {
		s.Mu.Unlock()
		return nil, err
	}
	a := Account{}
	if op.Account {
		a, err = s.allowed(p, contract.Str(args["account_alias"]))
		if err != nil {
			s.Mu.Unlock()
			return nil, err
		}
		if err = s.checkID(op, args, a.Alias); err != nil {
			s.Mu.Unlock()
			return nil, err
		}
	}
	if op.ID == "getAccounts" {
		allowed, e := s.accounts(p)
		if e != nil && s.Broker.Ready() {
			s.Mu.Unlock()
			return nil, e
		}
		if len(allowed) == 0 {
			s.Mu.Unlock()
			m := s.meta()
			m["account_selection_required"] = true
			m["source"] = "local_policy"
			return Object{"data": []any{}, "meta": m}, nil
		}
	}
	s.Mu.Unlock()
	if op.Account {
		ctx = s.accountReadContext(ctx, p, a.Alias, rev)
	}
	data, err := s.Broker.Query(ctx, op, args, a.Seq)
	if err != nil {
		return nil, err
	}
	s.Mu.Lock()
	defer s.Mu.Unlock()
	current, err := s.revision()
	if err != nil {
		return nil, err
	}
	if current != rev {
		return nil, fault.New("PERMISSION_DENIED", "account access changed during query")
	}
	if op.Account {
		if _, err = s.allowed(p, a.Alias); err != nil {
			return nil, err
		}
	}
	if op.ID == "getAccounts" {
		present := map[string]bool{}
		for _, v := range contract.List(data) {
			present[fmt.Sprint(contract.Obj(v)["accountSeq"])] = true
		}
		allowed, e := s.accounts(p)
		if e != nil {
			return nil, e
		}
		out := []Account{}
		for _, x := range allowed {
			if !present[x.Seq] {
				_, e = s.Store.DB.Exec("UPDATE accounts SET active=0 WHERE alias=?", x.Alias)
				if e != nil {
					return nil, e
				}
				_, e = s.Store.DB.Exec("UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='revision'")
				if e != nil {
					return nil, e
				}
				if s.Streams != nil {
					s.Streams.Notify()
				}
			} else {
				out = append(out, x)
			}
		}
		data = out
	}
	if op.Account {
		if _, requested := resourceID(op, args); requested != "" {
			key := "orderId"
			if op.ID == "getConditionalOrder" {
				key = "conditionalOrderId"
			}
			if contract.Str(contract.Obj(data)[key]) != requested {
				return nil, fault.New("UPSTREAM_DATA_INVALID", "response resource identifier mismatch")
			}
		}
		if err = s.recordIDs(op, data, a.Alias); err != nil {
			return nil, err
		}
	}
	m := s.meta()
	m["source"] = s.Backend + "_rest"
	if op.ID == "getAccounts" {
		m["account_selection_required"] = len(data.([]Account)) == 0
	}
	if a.Alias != "" {
		m["account_alias"] = a.Alias
		s.syncMeta(a.Alias, m)
	}
	if strings.Contains(op.ID, "Order") {
		m["coverage_limitations"] = []string{"official API supported order types only; not a complete account ledger", "REST pages are not an atomic snapshot"}
	}
	return Object{"data": sanitize(data), "meta": m}, nil
}
func sanitize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		o := Object{}
		for k, a := range x {
			if k == "accountSeq" || k == "accountNo" {
				continue
			}
			o[k] = sanitize(a)
		}
		return o
	case []any:
		a := []any{}
		for _, i := range x {
			a = append(a, sanitize(i))
		}
		return a
	}
	return v
}
func (s *Service) checkID(op contract.Operation, args Object, alias string) error {
	kind, id := resourceID(op, args)
	if id == "" {
		return nil
	}
	var owner string
	var conflict bool
	err := s.Store.DB.QueryRow("SELECT alias,conflict FROM relations WHERE client=? AND kind=? AND id=?", s.Broker.Identity(), kind, id).Scan(&owner, &conflict)
	if err == nil && (conflict || owner != alias) {
		return fault.New("PERMISSION_DENIED", "resource denied")
	}
	return nil
}
func resourceID(op contract.Operation, args Object) (string, string) {
	if op.ID == "getOrder" {
		return "order", contract.Str(args["orderId"])
	}
	if op.ID == "getConditionalOrder" {
		return "conditional", contract.Str(args["conditionalOrderId"])
	}
	return "", ""
}
func (s *Service) record(kind, id, alias string) error {
	if id == "" {
		return nil
	}
	_, err := s.Store.DB.Exec(`INSERT INTO relations(client,kind,id,alias) VALUES(?,?,?,?) ON CONFLICT(client,kind,id) DO UPDATE SET conflict=CASE WHEN relations.alias!=excluded.alias THEN 1 ELSE relations.conflict END`, s.Broker.Identity(), kind, id, alias)
	if err != nil {
		return err
	}
	var conflict bool
	if err = s.Store.DB.QueryRow("SELECT conflict FROM relations WHERE client=? AND kind=? AND id=?", s.Broker.Identity(), kind, id).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return fault.New("UPSTREAM_DATA_INVALID", "conflicting account relationship")
	}
	return nil
}
func (s *Service) recordIDs(op contract.Operation, data any, alias string) error {
	m := contract.Obj(data)
	switch op.ID {
	case "getOrder":
		return s.record("order", contract.Str(m["orderId"]), alias)
	case "getConditionalOrder":
		return s.record("conditional", contract.Str(m["conditionalOrderId"]), alias)
	case "getOrders":
		for _, v := range contract.List(m["orders"]) {
			if err := s.record("order", contract.Str(contract.Obj(v)["orderId"]), alias); err != nil {
				return err
			}
		}
	case "getConditionalOrders":
		for _, v := range contract.List(m["conditionalOrders"]) {
			if err := s.record("conditional", contract.Str(contract.Obj(v)["conditionalOrderId"]), alias); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) syncMeta(alias string, m Object) {
	var gap, truncated bool
	var status string
	var synced *string
	err := s.Store.DB.QueryRow("SELECT gap,status,last_synced,truncated FROM sync WHERE alias=?", alias).Scan(&gap, &status, &synced, &truncated)
	if err != nil {
		gap = true
		status = "recovering"
	}
	m["gap_detected"] = gap
	m["sync_status"] = status
	m["last_synced_at"] = synced
	m["history_truncated"] = truncated
}
func SortedKeys(m map[string]bool) []string {
	a := []string{}
	for k, v := range m {
		if v {
			a = append(a, k)
		}
	}
	sort.Strings(a)
	return a
}
func clone(v any) any { b, _ := json.Marshal(v); var x any; _ = contract.Decode(b, &x); return x }

// accountReadContext applies the same final-dispatch permission check to user
// queries and internal recovery reads. A request already in flight may finish,
// but a revoked or changed grant must not authorize another network request.
func (s *Service) accountReadContext(ctx context.Context, p Principal, alias string, revision int64) context.Context {
	return toss.WithDispatchGuard(ctx, func() error {
		s.Mu.Lock()
		defer s.Mu.Unlock()
		current, err := s.revision()
		if err != nil {
			return err
		}
		if current != revision {
			return fault.New("PERMISSION_DENIED", "access changed before dispatch")
		}
		_, err = s.allowed(p, alias)
		return err
	})
}
