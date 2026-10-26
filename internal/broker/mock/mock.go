// Package mock never reads Keychain or contacts external services.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"time"
)

type secret struct{}

func (secret) Read(context.Context) (toss.Credentials, error) {
	return toss.Credentials{ClientID: "mock-only-client", ClientSecret: "mock-only-secret"}, nil
}

type Server struct {
	Catalog     *contract.Catalog
	Server      *httptest.Server
	REST        *toss.REST
	mu          sync.Mutex
	Calls       map[string]int
	Status      map[string]int
	Hook        func(*http.Request)
	WSHook      func(*websocket.Conn)
	Connections int
	sockets     map[*websocket.Conn]bool
}

func New(c *contract.Catalog) *Server {
	m := &Server{Catalog: c, Calls: map[string]int{}, Status: map[string]int{}, sockets: map[*websocket.Conn]bool{}}
	m.Server = httptest.NewServer(http.HandlerFunc(m.ServeHTTP))
	m.REST = toss.NewRESTForTransport(c, secret{}, m.Server.URL, http.DefaultTransport)
	return m
}
func (m *Server) Close() { m.Server.Close() }
func (m *Server) WSURL() string {
	return strings.Replace(m.Server.URL, "http://", "ws://", 1) + "/ws/v1"
}
func (m *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws/v1" {
		m.serveWS(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "POST" && r.URL.Path == "/oauth2/token" {
		_ = r.ParseForm()
		if r.Form.Get("client_id") != "mock-only-client" || r.Form.Get("client_secret") != "mock-only-secret" || r.Form.Get("grant_type") != "client_credentials" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"invalid_client"}`)
			return
		}
		m.mu.Lock()
		m.Calls["token"]++
		m.mu.Unlock()
		fmt.Fprint(w, `{"access_token":"mock-only-access","token_type":"Bearer","expires_in":86400}`)
		return
	}
	for _, op := range m.Catalog.Ops {
		if op.Method != "GET" || r.Method != "GET" {
			continue
		}
		a, b := strings.Split(r.URL.Path, "/"), strings.Split(op.Path, "/")
		if len(a) != len(b) {
			continue
		}
		match := true
		for i, p := range b {
			if !strings.HasPrefix(p, "{") && p != a[i] {
				match = false
			}
		}
		if !match {
			continue
		}
		m.mu.Lock()
		m.Calls[op.ID]++
		status := m.Status[op.ID]
		hook := m.Hook
		m.mu.Unlock()
		if hook != nil {
			hook(r)
		}
		if r.Header.Get("Authorization") != "Bearer mock-only-access" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"code":"unauthorized","requestId":"mock-request","message":"unauthorized"}}`)
			return
		}
		if status != 0 && status != 200 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"code":"mock-error","requestId":"mock-request","message":"mock error","data":{"field":"symbol","unknownHint":"preserved internally"}}}`)
			return
		}
		if op.Account && r.Header.Get("X-Tossinvest-Account") != "1" && r.Header.Get("X-Tossinvest-Account") != "2" {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"code":"account-denied","requestId":"mock-request","message":"denied"}}`)
			return
		}
		if op.ID == "getAccounts" {
			fmt.Fprint(w, `{"result":[{"accountNo":"00000001111","accountSeq":1,"accountType":"BROKERAGE"},{"accountNo":"00000002222","accountSeq":2,"accountType":"FUTURE_ACCOUNT_TYPE"}]}`)
			return
		}
		examples := m.Catalog.Examples(op, "200")
		if len(examples) == 0 {
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":{"code":"missing-example","requestId":"mock-request","message":"missing official example"}}`)
			return
		}
		// Choose a deterministic nonempty official response. Tests exercise all examples separately.
		sort.SliceStable(examples, func(i, j int) bool {
			a, _ := json.Marshal(examples[i])
			b, _ := json.Marshal(examples[j])
			if len(a) != len(b) {
				return len(a) > len(b)
			}
			return string(a) < string(b)
		})
		value := examples[0]
		bdy, _ := json.Marshal(value)
		var result contract.Object
		_ = contract.Decode(bdy, &result)
		if op.ID == "getOrder" || op.ID == "getConditionalOrder" {
			id := a[len(a)-1]
			if strings.HasPrefix(id, "missing") || strings.HasPrefix(id, "account-2") && r.Header.Get("X-Tossinvest-Account") != "2" {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":{"code":"order-not-found","requestId":"mock-request","message":"not found"}}`)
				return
			}
			key := "orderId"
			if op.ID == "getConditionalOrder" {
				key = "conditionalOrderId"
			}
			contract.Obj(result["result"])[key] = id
		}
		if op.ID == "getOrders" || op.ID == "getConditionalOrders" {
			m := contract.Obj(result["result"])
			m["hasNext"] = false
			m["nextCursor"] = nil
		}
		_ = json.NewEncoder(w).Encode(result)
		return
	}
	w.WriteHeader(404)
}
func (m *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer mock-only-access" {
		http.Error(w, "unauthorized", 401)
		return
	}
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"127.0.0.1:*"}})
	if e != nil {
		return
	}
	defer c.CloseNow()
	m.mu.Lock()
	m.Connections++
	if m.sockets == nil {
		m.sockets = map[*websocket.Conn]bool{}
	}
	m.sockets[c] = true
	defer func() { m.mu.Lock(); delete(m.sockets, c); m.mu.Unlock() }()
	hook := m.WSHook
	m.mu.Unlock()
	if hook != nil {
		hook(c)
		return
	}
	ctx := r.Context()
	c.SetReadLimit(1 << 20)
	for {
		_, b, e := c.Read(ctx)
		if e != nil {
			return
		}
		if string(b) == "PING" {
			if c.Write(ctx, websocket.MessageText, []byte(`{"type":"pong"}`)) != nil {
				return
			}
			continue
		}
		var declarations []contract.Object
		if contract.Decode(b, &declarations) != nil {
			_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","error":{"code":"wrong-format","message":"expected array"}}`))
			continue
		}
		subscribed := []string{}
		rejected := []any{}
		id := ""
		for _, d := range declarations {
			if v := contract.Str(d["id"]); v != "" {
				id = v
			}
			for _, code := range contract.List(d["codes"]) {
				key := contract.Str(d["type"]) + ":" + contract.Str(code)
				if strings.Contains(key, "NOPE") {
					rejected = append(rejected, contract.Object{"target": key, "code": "stock-not-found", "message": "mock rejection"})
				} else {
					subscribed = append(subscribed, key)
				}
			}
		}
		ack, _ := json.Marshal(contract.Object{"type": "subscriptions", "id": id, "subscribed": subscribed, "rejected": rejected})
		if c.Write(ctx, websocket.MessageText, ack) != nil {
			return
		}
		for _, topic := range subscribed {
			parts := strings.Split(topic, ":")
			if len(parts) != 3 {
				continue
			}
			data := contract.Object{}
			if parts[0] == "personal" {
				ch := contract.Obj(contract.Obj(m.Catalog.Async["channels"])["realtime-order"])
				message := contract.Obj(contract.Obj(ch["messages"])["orderStream"])
				examples := contract.List(message["examples"])
				data = contract.Obj(contract.Obj(contract.Obj(examples[0])["payload"])["data"])
				body, _ := json.Marshal(data)
				_ = contract.Decode(body, &data)
				data["accountSeq"] = parts[2]
				contract.Obj(data["order"])["orderId"] = "mock-ws-order-" + parts[2]
			} else {
				currency := "USD"
				if parts[1] == "kr" {
					currency = "KRW"
				}
				data = contract.Object{"price": "123.4500", "volume": "0.000001", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "currency": currency}
				if parts[0] == "orderbook" {
					data = contract.Object{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "currency": currency, "asks": []any{contract.Object{"price": "124.00", "volume": "3"}}, "bids": []any{contract.Object{"price": "123.00", "volume": "4"}}}
				}
			}
			frame, _ := json.Marshal(contract.Object{"type": "message", "topic": topic, "data": data})
			if c.Write(ctx, websocket.MessageText, frame) != nil {
				return
			}
		}
	}
}

func (m *Server) ConnectionCount() int { m.mu.Lock(); defer m.mu.Unlock(); return m.Connections }
func (m *Server) Send(frame contract.Object) {
	b, _ := json.Marshal(frame)
	m.mu.Lock()
	sockets := []*websocket.Conn{}
	for c := range m.sockets {
		sockets = append(sockets, c)
	}
	m.mu.Unlock()
	for _, c := range sockets {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = c.Write(ctx, websocket.MessageText, b)
		cancel()
	}
}
func (m *Server) Disconnect() {
	m.mu.Lock()
	sockets := []*websocket.Conn{}
	for c := range m.sockets {
		sockets = append(sockets, c)
	}
	m.mu.Unlock()
	for _, c := range sockets {
		_ = c.CloseNow()
	}
}

func (m *Server) SendRaw(b []byte) {
	m.mu.Lock()
	sockets := []*websocket.Conn{}
	for c := range m.sockets {
		sockets = append(sockets, c)
	}
	m.mu.Unlock()
	for _, c := range sockets {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = c.Write(ctx, websocket.MessageText, b)
		cancel()
	}
}
func (m *Server) CallCount(id string) int { m.mu.Lock(); defer m.mu.Unlock(); return m.Calls[id] }
