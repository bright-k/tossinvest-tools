package core

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/bright-k/tossinvest-tools/internal/store"
	"github.com/coder/websocket"
)

type queuedEvent struct {
	Alias               string
	Payload             []byte
	At                  time.Time
	Version, Generation int64
}
type EventQueue struct {
	mu                 sync.Mutex
	items              []queuedEvent
	bytes              int
	Wake               chan struct{}
	MaxCount, MaxBytes int
	MaxWait            time.Duration
}

func NewEventQueue() *EventQueue {
	return &EventQueue{Wake: make(chan struct{}, 1), MaxCount: 1024, MaxBytes: 8 << 20, MaxWait: time.Second}
}
func (q *EventQueue) Push(v queuedEvent, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) >= q.MaxCount || q.bytes+len(v.Payload) > q.MaxBytes || len(q.items) > 0 && now.Sub(q.items[0].At) >= q.MaxWait {
		return false
	}
	q.items = append(q.items, v)
	q.bytes += len(v.Payload)
	select {
	case q.Wake <- struct{}{}:
	default:
	}
	return true
}
func (q *EventQueue) Pop() (queuedEvent, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return queuedEvent{}, false
	}
	v := q.items[0]
	q.items[0] = queuedEvent{}
	q.items = q.items[1:]
	q.bytes -= len(v.Payload)
	return v, true
}
func (q *EventQueue) Expired(now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items) > 0 && now.Sub(q.items[0].At) >= q.MaxWait
}

type StreamOptions struct {
	URL        string
	Ping       time.Duration
	Backoff    time.Duration
	Token      func(context.Context, string) (string, error)
	DialClient *http.Client
}
type Streams struct {
	Service           *Service
	Options           StreamOptions
	mu                sync.Mutex
	changed           chan struct{}
	done              chan struct{}
	writerDone        chan struct{}
	cancel            context.CancelFunc
	queue             *EventQueue
	conn              *websocket.Conn
	connected         bool
	generation        int64
	gap               bool
	dbErr             bool
	lastError         string
	lastPong          time.Time
	watch             map[string]bool
	active            map[string]bool
	blocked           map[string]string
	aliases           map[string]string
	versions          map[string]int64
	committed         map[string]int64
	synced            map[string]bool
	snapshots         map[string]Object
	lastAckGeneration int64
	pendingID         string
	pending           map[string]bool
	pendingAt         time.Time
	lastDeclare       time.Time
	baseCtx           context.Context
	syncRunning       bool
	syncAgain         bool
	wg                sync.WaitGroup
}

func NewStreams(s *Service, o StreamOptions) *Streams {
	if o.Ping <= 0 {
		o.Ping = 60 * time.Second
	}
	if o.Backoff <= 0 {
		o.Backoff = time.Second
	}
	if o.DialClient == nil {
		o.DialClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("WS redirect denied") }}
	}
	x := &Streams{Service: s, Options: o, changed: make(chan struct{}, 1), done: make(chan struct{}), writerDone: make(chan struct{}), queue: NewEventQueue(), gap: true, watch: map[string]bool{}, active: map[string]bool{}, blocked: map[string]string{}, aliases: map[string]string{}, versions: map[string]int64{}, committed: map[string]int64{}, synced: map[string]bool{}, snapshots: map[string]Object{}}
	rows, e := s.Store.DB.Query("SELECT topic FROM watchlist WHERE principal=?", PrincipalID)
	if e == nil {
		for rows.Next() {
			var topic string
			if rows.Scan(&topic) == nil {
				x.watch[topic] = true
			}
		}
		rows.Close()
	}
	return x
}
func (x *Streams) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	x.cancel = cancel
	x.baseCtx = ctx
	go x.writer(ctx)
	go x.run(ctx)
}
func (x *Streams) Close() {
	if x.cancel != nil {
		x.cancel()
	}
	x.mu.Lock()
	c := x.conn
	x.mu.Unlock()
	if c != nil {
		_ = c.CloseNow()
	}
	<-x.done
	<-x.writerDone
	x.wg.Wait()
}
func (x *Streams) Notify() {
	select {
	case x.changed <- struct{}{}:
	default:
	}
}
func (x *Streams) ReadyLocked() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.connected || x.dbErr || x.pendingID != "" {
		return false
	}
	for _, alias := range x.aliases {
		if !x.synced[alias] {
			return false
		}
	}
	for topic := range x.watch {
		if !x.active[topic] {
			return false
		}
	}
	for seq := range x.aliases {
		if !x.active["personal:order:"+seq] {
			return false
		}
	}
	return true
}
func (x *Streams) StatusLocked(p Principal) Object {
	allowed := map[string]bool{}
	if p.Scopes["accounts:read"] {
		accounts, e := x.Service.accounts(p)
		if e == nil {
			for _, a := range accounts {
				allowed[a.Alias] = true
			}
		}
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	active := []string{}
	rejected := []any{}
	for topic := range x.active {
		if strings.HasPrefix(topic, "personal:order:") {
			seq := strings.TrimPrefix(topic, "personal:order:")
			if alias := x.aliases[seq]; alias != "" && allowed[alias] {
				active = append(active, "personal:order:"+alias)
			}
		} else {
			active = append(active, topic)
		}
	}
	sort.Strings(active)
	for topic, code := range x.blocked {
		if strings.HasPrefix(topic, "personal:order:") {
			alias := x.aliases[strings.TrimPrefix(topic, "personal:order:")]
			if alias == "" || !allowed[alias] {
				continue
			}
			topic = "personal:order:" + alias
		}
		rejected = append(rejected, Object{"target": topic, "code": code})
	}
	states := Object{}
	if p.Scopes["accounts:read"] {
		for _, alias := range x.aliases {
			if !allowed[alias] {
				continue
			}
			state := "recovering"
			if x.synced[alias] {
				state = "synced"
			}
			states[alias] = Object{"sync_status": state, "gap_detected": x.gap}
		}
	}
	return Object{"connected": x.connected, "generation": x.generation, "gap_detected": x.gap, "storage_available": !x.dbErr, "last_error": x.lastError, "active_subscriptions": active, "rejected": rejected, "account_sync": states, "last_pong_at": x.lastPong}
}
func (x *Streams) Call(ctx context.Context, p Principal, tool string, args Object) (Object, bool, error) {
	schema, ok := StreamSchemas()[tool]
	if !ok {
		return nil, false, nil
	}
	// Schemas are constructed in Go; normalize so all constraints share the JSON-number representation.
	normalized := contract.Obj(clone(schema))
	if e := x.Service.Catalog.Validate(normalized, args, true); e != nil {
		return nil, true, fault.New("INVALID_ARGUMENT", e.Error())
	}
	scope := "streams:read"
	if tool == "tossinvest_set_subscriptions" {
		scope = "streams:manage"
	}
	if e := p.Require(scope); e != nil {
		return nil, true, e
	}
	switch tool {
	case "tossinvest_list_order_events":
		d, e := x.Service.Events(ctx, p, args)
		return d, true, e
	case "tossinvest_get_stream_status":
		x.Service.Mu.Lock()
		d := x.StatusLocked(p)
		x.Service.Mu.Unlock()
		return Object{"data": d, "meta": x.Service.meta()}, true, nil
	case "tossinvest_set_subscriptions":
		d, e := x.set(p, args)
		return d, true, e
	case "tossinvest_get_stream_snapshot":
		topic := contract.Str(args["channel"]) + ":" + contract.Str(args["symbol"])
		x.mu.Lock()
		v, ok := x.snapshots[topic]
		gap, connected := x.gap, x.connected
		watch := x.watch[topic]
		x.mu.Unlock()
		if !watch {
			return nil, true, fault.New("PERMISSION_DENIED", "snapshot outside your watchlist")
		}
		if !ok {
			return nil, true, fault.New("NOT_READY", "snapshot not observed")
		}
		m := x.Service.meta()
		m["gap_detected"] = gap
		if !connected {
			m["stale"] = true
		} // A live connection alone cannot establish data freshness.
		m["data_timestamp"] = contract.Obj(v["data"])["timestamp"]
		return Object{"data": clone(v), "meta": m}, true, nil
	}
	return nil, true, fault.New("INVALID_ARGUMENT", "unknown stream operation")
}
func (x *Streams) set(p Principal, args Object) (Object, error) {
	s := x.Service
	s.Mu.Lock()
	defer s.Mu.Unlock()
	watch := map[string]bool{}
	count := 0
	for _, v := range contract.List(args["subscriptions"]) {
		m := contract.Obj(v)
		ch := contract.Str(m["channel"])
		codes := contract.List(m["codes"])
		if len(codes) == 0 {
			return nil, fault.New("INVALID_ARGUMENT", "codes must not be empty")
		}
		for _, c := range codes {
			code := contract.Str(c)
			if len(code) > 64 {
				return nil, fault.New("INVALID_ARGUMENT", "code too long")
			}
			if ch == "personal:order" {
				if _, e := s.allowed(p, code); e != nil {
					return nil, e
				}
				continue
			}
			pattern := `^[A-Z][A-Z0-9.\-]{0,31}$`
			if strings.HasSuffix(ch, ":kr") {
				pattern = `^[0-9A-Z]{6}$`
			}
			if !regexp.MustCompile(pattern).MatchString(code) {
				return nil, fault.New("INVALID_ARGUMENT", "symbol format or market mismatch")
			}
			watch[ch+":"+code] = true
		}
	}
	accounts, e := s.accounts(p)
	if e != nil {
		return nil, e
	}
	count = len(watch) + len(accounts)
	if count > 100 {
		return nil, fault.New("INVALID_ARGUMENT", "100 subscription maximum including mandatory orders")
	}
	tx, e := s.Store.DB.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM watchlist WHERE principal=?", p.ID); e != nil {
		return nil, e
	}
	for topic := range watch {
		if _, e = tx.Exec("INSERT INTO watchlist VALUES(?,?)", p.ID, topic); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	x.mu.Lock()
	x.watch = watch
	x.blocked = map[string]string{}
	for topic := range x.snapshots {
		if !watch[topic] {
			delete(x.snapshots, topic)
		}
	}
	x.mu.Unlock()
	x.Notify()
	return Object{"data": Object{"watchlist": SortedKeys(watch), "mandatory_order_subscriptions": len(accounts), "declaration_pending": true}, "meta": s.meta()}, nil
}
func (x *Streams) desired() (map[string]bool, map[string]string, error) {
	s := x.Service
	s.Mu.Lock()
	accounts, e := s.accounts(DefaultPrincipal())
	s.Mu.Unlock()
	if e != nil {
		return nil, nil, e
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	desired := map[string]bool{}
	aliases := map[string]string{}
	for topic := range x.watch {
		if x.blocked[topic] == "" {
			desired[topic] = true
		}
	}
	for _, a := range accounts {
		aliases[a.Seq] = a.Alias
		topic := "personal:order:" + a.Seq
		if x.blocked[topic] == "" {
			desired[topic] = true
		}
	}
	if len(desired) > 100 {
		return nil, nil, fault.New("NOT_READY", "mandatory order subscriptions exceed limit")
	}
	return desired, aliases, nil
}
func (x *Streams) declare(ctx context.Context, c *websocket.Conn) error {
	desired, aliases, e := x.desired()
	if e != nil {
		return e
	}
	x.mu.Lock()
	same := len(desired) == len(x.active) && len(aliases) == len(x.aliases)
	for k := range desired {
		if !x.active[k] {
			same = false
		}
	}
	for k, v := range aliases {
		if x.aliases[k] != v {
			same = false
		}
	}
	x.aliases = aliases
	if same && x.lastAckGeneration == x.generation {
		x.mu.Unlock()
		return nil
	}
	if x.pendingID != "" {
		x.mu.Unlock()
		return nil
	}
	id := Random()
	x.pendingID = id
	x.pending = desired
	x.pendingAt = time.Now()
	x.lastDeclare = time.Now()
	x.mu.Unlock()
	groups := map[string][]string{}
	for _, topic := range SortedKeys(desired) {
		parts := strings.Split(topic, ":")
		groups[strings.Join(parts[:2], ":")] = append(groups[strings.Join(parts[:2], ":")], parts[2])
	}
	decl := []any{Object{"id": id}}
	keys := []string{}
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, ch := range keys {
		decl = append(decl, Object{"type": ch, "codes": groups[ch]})
	}
	b, _ := json.Marshal(decl)
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.Write(writeCtx, websocket.MessageText, b)
}
func (x *Streams) fail(code string) {
	x.mu.Lock()
	x.gap = true
	x.lastError = code
	for alias := range x.synced {
		x.synced[alias] = false
	}
	c := x.conn
	x.mu.Unlock()
	if c != nil {
		_ = c.CloseNow()
	}
}
func jitter(base time.Duration) time.Duration {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return base + time.Duration(binary.LittleEndian.Uint64(b[:])%uint64(base/2+1))
}
func (x *Streams) run(ctx context.Context) {
	defer close(x.done)
	backoff := x.Options.Backoff
	rejected := ""
	for ctx.Err() == nil {
		token, e := x.Options.Token(ctx, rejected)
		if e != nil {
			x.fail("TOKEN_UNAVAILABLE")
			if sleep(ctx, jitter(backoff)) != nil {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		rejected = ""
		c, resp, e := websocket.Dial(ctx, x.Options.URL, &websocket.DialOptions{HTTPClient: x.Options.DialClient, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
		if e != nil {
			if resp != nil && resp.StatusCode == 401 {
				rejected = token
			}
			x.fail("WS_CONNECT_FAILED")
			if sleep(ctx, jitter(backoff)) != nil {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		c.SetReadLimit(1 << 20)
		x.mu.Lock()
		x.conn = c
		x.connected = true
		x.generation++
		x.active = map[string]bool{}
		x.pendingID = ""
		x.synced = map[string]bool{}
		x.lastPong = time.Time{}
		x.mu.Unlock()
		connectionCtx, cancel := context.WithCancel(ctx)
		errch := make(chan error, 2)
		var connWG sync.WaitGroup
		connWG.Add(2)
		go func() { defer connWG.Done(); errch <- x.read(connectionCtx, c) }()
		go func() { defer connWG.Done(); errch <- x.heartbeat(connectionCtx, c) }()
		e = x.declare(connectionCtx, c)
		timer := time.NewTicker(100 * time.Millisecond)
	loop:
		for e == nil {
			select {
			case <-ctx.Done():
				e = ctx.Err()
				break loop
			case e = <-errch:
				break loop
			case <-x.changed:
				x.mu.Lock()
				wait := time.Until(x.lastDeclare.Add(210 * time.Millisecond))
				x.mu.Unlock()
				if wait > 0 {
					if sleep(connectionCtx, wait) != nil {
						break loop
					}
				}
				e = x.declare(connectionCtx, c)
			case <-timer.C:
				x.mu.Lock()
				pending := x.pendingID != ""
				at := x.pendingAt
				x.mu.Unlock()
				if pending && time.Since(at) > 10*time.Second {
					e = fmt.Errorf("ACK timeout")
					break loop
				}
				if x.queue.Expired(time.Now()) {
					e = fmt.Errorf("event queue age exceeded")
					x.fail("EVENT_QUEUE_OVERFLOW")
					break loop
				}
			}
		}
		timer.Stop()
		cancel()
		_ = c.CloseNow()
		connWG.Wait()
		x.mu.Lock()
		x.connected = false
		x.conn = nil
		x.pendingID = ""
		x.gap = true
		x.mu.Unlock()
		x.persistGap()
		if ctx.Err() != nil {
			return
		}
		if time.Since(x.lastDeclare) > 5*time.Second {
			backoff = x.Options.Backoff
		}
		if sleep(ctx, jitter(backoff)) != nil {
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (x *Streams) heartbeat(ctx context.Context, c *websocket.Conn) error {
	ticker := time.NewTicker(x.Options.Ping)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			e := c.Write(writeCtx, websocket.MessageText, []byte("PING"))
			cancel()
			if e != nil {
				return e
			}
			x.mu.Lock()
			pong := x.lastPong
			started := x.lastDeclare
			x.mu.Unlock()
			if !pong.IsZero() && time.Since(pong) > 2*x.Options.Ping || pong.IsZero() && time.Since(started) > 3*x.Options.Ping {
				return fmt.Errorf("pong timeout")
			}
		}
	}
}
func (x *Streams) read(ctx context.Context, c *websocket.Conn) error {
	for {
		typ, b, e := c.Read(ctx)
		if e != nil {
			return e
		}
		if typ != websocket.MessageText || len(b) > 1<<20 {
			return fmt.Errorf("invalid frame")
		}
		if e = x.process(ctx, b); e != nil {
			x.fail("WS_DATA_OR_STORAGE_INVALID")
			return e
		}
	}
}
func (x *Streams) process(ctx context.Context, b []byte) error {
	var frame Object
	if contract.Decode(b, &frame) != nil {
		return fmt.Errorf("invalid JSON frame")
	}
	typ := contract.Str(frame["type"])
	switch typ {
	case "pong":
		x.mu.Lock()
		x.lastPong = time.Now()
		x.mu.Unlock()
		return nil
	case "subscriptions":
		if e := x.validateFrame("connection", "subscriptionsAck", frame); e != nil {
			return e
		}
		x.mu.Lock()
		if contract.Str(frame["id"]) != x.pendingID || x.pendingID == "" {
			x.mu.Unlock()
			return nil
		}
		active := map[string]bool{}
		seen := map[string]bool{}
		for _, v := range contract.List(frame["subscribed"]) {
			topic := contract.Str(v)
			if !x.pending[topic] || seen[topic] {
				x.mu.Unlock()
				return fmt.Errorf("unexpected ack topic")
			}
			active[topic] = true
			seen[topic] = true
		}
		for _, v := range contract.List(frame["rejected"]) {
			m := contract.Obj(v)
			topic := contract.Str(m["target"])
			if !x.pending[topic] || seen[topic] {
				x.mu.Unlock()
				return fmt.Errorf("unexpected rejected topic")
			}
			seen[topic] = true
			x.blocked[topic] = contract.Str(m["code"])
		}
		if len(seen) != len(x.pending) {
			x.mu.Unlock()
			return fmt.Errorf("incomplete ack")
		}
		x.active = active
		x.lastAckGeneration = x.generation
		x.pendingID = ""
		x.mu.Unlock()
		x.startSync(ctx)
		x.Notify()
		return nil
	case "error":
		if e := x.validateFrame("connection", "errorFrame", frame); e != nil {
			return e
		}
		code := contract.Str(contract.Obj(frame["error"])["code"])
		if code == "server-shutdown" {
			return fmt.Errorf("server shutdown")
		}
		x.mu.Lock()
		x.lastError = code
		if id := contract.Str(frame["id"]); id != "" && id == x.pendingID {
			x.pendingID = ""
		}
		x.mu.Unlock()
		if code == "rate-limit-exceeded" {
			go func() {
				if sleep(ctx, time.Second) == nil {
					x.Notify()
				}
			}()
		}
		return nil
	case "message":
		topic := contract.Str(frame["topic"])
		parts := strings.Split(topic, ":")
		if len(parts) != 3 {
			return fmt.Errorf("invalid topic")
		}
		ch := "realtime-trade"
		message := "tradeStream"
		if parts[0] == "orderbook" {
			ch = "realtime-orderbook"
			message = "orderbookStream"
		} else if parts[0] == "personal" && parts[1] == "order" {
			ch = "realtime-order"
			message = "orderStream"
		} else if parts[0] != "trade" {
			return fmt.Errorf("unknown data topic")
		}
		if e := x.validateFrame(ch, message, frame); e != nil {
			return e
		}
		x.mu.Lock()
		if !x.active[topic] {
			x.mu.Unlock()
			return nil
		}
		if parts[0] != "personal" {
			x.snapshots[topic] = Object{"topic": topic, "data": frame["data"], "received_at": time.Now().UTC().Format(time.RFC3339Nano)}
			x.mu.Unlock()
			return nil
		}
		alias := x.aliases[parts[2]]
		if alias == "" || contract.Str(contract.Obj(frame["data"])["accountSeq"]) != parts[2] {
			x.mu.Unlock()
			return fmt.Errorf("event account mismatch")
		}
		x.versions[alias]++
		version, gen := x.versions[alias], x.generation
		x.mu.Unlock()
		payload, _ := json.Marshal(frame["data"])
		if !x.queue.Push(queuedEvent{alias, payload, time.Now(), version, gen}, time.Now()) {
			x.fail("EVENT_QUEUE_OVERFLOW")
			return fmt.Errorf("event queue overflow")
		}
		return nil
	default:
		return fmt.Errorf("unknown frame type")
	}
}
func (x *Streams) validateFrame(channel, message string, v Object) error {
	ch := contract.Obj(contract.Obj(x.Service.Catalog.Async["channels"])[channel])
	schema := contract.Obj(contract.Obj(contract.Obj(ch["messages"])[message])["payload"])
	return x.Service.Catalog.Validate(schema, v, false)
}
func (x *Streams) persistGap() {
	s := x.Service
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if _, e := s.Store.DB.Exec("UPDATE sync SET gap=1,status='recovering'"); e != nil {
		x.mu.Lock()
		x.dbErr = true
		x.mu.Unlock()
	}
}
func (x *Streams) writer(ctx context.Context) {
	defer close(x.writerDone)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s := x.Service
			s.Mu.Lock()
			e := s.Store.Prune(s.Now(), store.DefaultHistory)
			s.Mu.Unlock()
			if e != nil {
				x.mu.Lock()
				x.dbErr = true
				x.mu.Unlock()
				x.fail("EVENT_STORE_UNAVAILABLE")
			}
		case <-x.queue.Wake:
			for {
				v, ok := x.queue.Pop()
				if !ok {
					break
				}
				s := x.Service
				s.Mu.Lock()
				_, e := s.allowed(DefaultPrincipal(), v.Alias)
				if e == nil {
					var payload Object
					if contract.Decode(v.Payload, &payload) != nil {
						e = fmt.Errorf("invalid event")
					} else {
						e = s.record("order", contract.Str(contract.Obj(payload["order"])["orderId"]), v.Alias)
						if e == nil {
							e = s.Store.AppendEvent(v.Alias, v.At, v.Payload, store.DefaultHistory)
						}
					}
				}
				s.Mu.Unlock()
				if e != nil {
					if f, ok := e.(*fault.Error); ok && f.Code == "PERMISSION_DENIED" {
						continue
					}
					x.mu.Lock()
					x.dbErr = true
					x.mu.Unlock()
					x.fail("EVENT_STORE_UNAVAILABLE")
					continue
				}
				x.mu.Lock()
				x.committed[v.Alias] = max(x.committed[v.Alias], v.Version)
				x.dbErr = false
				x.mu.Unlock()
			}
		}
	}
}
func (x *Streams) startSync(ctx context.Context) {
	if x.baseCtx != nil {
		ctx = x.baseCtx
	}
	x.mu.Lock()
	if x.syncRunning {
		x.syncAgain = true
		x.mu.Unlock()
		return
	}
	x.syncRunning = true
	x.mu.Unlock()
	x.wg.Add(1)
	go func() {
		defer x.wg.Done()
		for {
			e := x.synchronize(ctx)
			x.mu.Lock()
			again := x.syncAgain
			x.syncAgain = false
			if e != nil {
				x.lastError = "REST_SYNC_FAILED"
			}
			x.syncRunning = false
			x.mu.Unlock()
			if e != nil {
				go func() {
					if sleep(ctx, time.Second) == nil {
						x.startSync(ctx)
					}
				}()
				return
			}
			if !again {
				return
			}
			x.mu.Lock()
			x.syncRunning = true
			x.mu.Unlock()
		}
	}()
}
func (x *Streams) synchronize(ctx context.Context) error {
	x.mu.Lock()
	aliases := map[string]string{}
	gen := x.generation
	for seq, a := range x.aliases {
		if x.active["personal:order:"+seq] {
			aliases[a] = seq
		}
	}
	x.mu.Unlock()
	for alias, seq := range aliases {
		s := x.Service
		s.Mu.Lock()
		account, accessErr := s.allowed(DefaultPrincipal(), alias)
		revision, revisionErr := s.revision()
		s.Mu.Unlock()
		if accessErr != nil {
			return accessErr
		}
		if revisionErr != nil {
			return revisionErr
		}
		if account.Seq != seq {
			return fault.New("PERMISSION_DENIED", "recovery account changed")
		}
		readCtx := s.accountReadContext(ctx, DefaultPrincipal(), alias, revision)
		success := false
		for tries := 0; tries < 3; tries++ {
			x.mu.Lock()
			version := x.versions[alias]
			x.mu.Unlock()
			snapshot := Object{}
			for _, id := range []string{"getHoldings", "getOrders", "getConditionalOrders"} {
				op, _ := x.Service.Catalog.Find(id)
				if id == "getHoldings" {
					args := Object{"account_alias": alias}
					data, e := x.Service.Broker.Query(readCtx, op, args, seq)
					if e != nil {
						return e
					}
					snapshot[id] = data
					continue
				}
				for _, status := range []string{"OPEN", "CLOSED"} {
					args := Object{"account_alias": alias, "status": status, "limit": json.Number("100")}
					pages := []any{}
					seen := map[string]bool{}
					for page := 0; page < 1000; page++ {
						data, e := x.Service.Broker.Query(readCtx, op, args, seq)
						if e != nil {
							return e
						}
						pages = append(pages, data)
						m := contract.Obj(data)
						if id == "getOrders" && status == "OPEN" {
							break
						}
						next := contract.Str(m["nextCursor"])
						if m["hasNext"] != true {
							break
						}
						if next == "" || seen[next] {
							return fmt.Errorf("invalid or repeated pagination cursor")
						}
						seen[next] = true
						args["cursor"] = next
						if page == 999 {
							return fmt.Errorf("recovery pagination exceeded")
						}
					}
					snapshot[id+status] = pages
				}
			}
			// The bounded reader runs throughout REST recovery. Publish a snapshot only when overlapping events have committed and the observation version stayed stable.
			waitUntil := time.Now().Add(time.Second)
			for {
				x.mu.Lock()
				committed := x.committed[alias]
				x.mu.Unlock()
				if committed >= version {
					break
				}
				if time.Now().After(waitUntil) {
					return fmt.Errorf("event commit overlap timeout")
				}
				if sleep(ctx, 10*time.Millisecond) != nil {
					return ctx.Err()
				}
			}
			s := x.Service
			s.Mu.Lock()
			currentRevision, revisionErr := s.revision()
			if revisionErr != nil {
				s.Mu.Unlock()
				return revisionErr
			}
			if currentRevision != revision {
				s.Mu.Unlock()
				return fault.New("PERMISSION_DENIED", "access changed during recovery")
			}
			if _, e := s.allowed(DefaultPrincipal(), alias); e != nil {
				s.Mu.Unlock()
				return e
			}
			x.mu.Lock()
			current := x.versions[alias]
			sameGen := x.generation == gen && x.connected
			x.mu.Unlock()
			if !sameGen {
				s.Mu.Unlock()
				return fmt.Errorf("connection generation changed")
			}
			if current != version {
				s.Mu.Unlock()
				continue
			}
			body, _ := json.Marshal(sanitize(snapshot))
			tx, e := s.Store.DB.Begin()
			if e != nil {
				s.Mu.Unlock()
				return e
			}
			_, e = tx.Exec("INSERT INTO snapshots(alias,generation,payload) VALUES(?,?,?) ON CONFLICT(alias) DO UPDATE SET generation=excluded.generation,payload=excluded.payload", alias, gen, body)
			if e == nil {
				_, e = tx.Exec("UPDATE sync SET gap=1,status='synced',last_synced=? WHERE alias=?", s.Now().UTC().Format(time.RFC3339Nano), alias)
			}
			if e == nil {
				e = tx.Commit()
			} else {
				tx.Rollback()
			}
			s.Mu.Unlock()
			if e != nil {
				return e
			}
			x.mu.Lock()
			x.synced[alias] = true
			x.dbErr = false
			x.mu.Unlock()
			success = true
			break
		}
		if !success {
			return fmt.Errorf("account changing during recovery")
		}
	}
	return nil
}
