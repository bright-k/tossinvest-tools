package runtime

import (
	"context"
	"encoding/json"
	"github.com/bright-k/tossinvest-tools/internal/broker/mock"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func root(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("/private/tmp", "ti-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}
func address(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}
func startTest(t *testing.T, milestone string) *Server {
	t.Helper()
	s, e := Start(context.Background(), Options{Root: root(t), Backend: "mock", Listen: address(t), Milestone: milestone})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}
func grant(t *testing.T, s *Server) {
	t.Helper()
	client, e := OwnerClient(s.Options.Root, "mock")
	if e != nil {
		t.Fatal(e)
	}
	d, e := OwnerRequest(context.Background(), client, "POST", "/owner/v1/accounts/discover", nil)
	if e != nil {
		t.Fatal(e)
	}
	id := contract.Str(d["discovery_id"])
	rev, _ := d["expected_access_revision"].(json.Number)
	revision, _ := rev.Int64()
	a := contract.Obj(contract.List(d["accounts"])[0])
	_, e = OwnerRequest(context.Background(), client, "PUT", "/owner/v1/accounts/grant", core.Grant{DiscoveryID: &id, Revision: revision, Principal: core.PrincipalID, Selected: []core.Selection{{Handle: contract.Str(a["handle"]), Alias: "primary"}}})
	if e != nil {
		t.Fatal(e)
	}
}
func TestR1MCPDiscoveryCallsAndAuth(t *testing.T) {
	s := startTest(t, "R1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "R1-contract-client", Version: "1.8.0"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: s.URL() + "/mcp", HTTPClient: s.ClientHTTP()}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil {
		t.Fatal(e)
	}
	if len(tools.Tools) != 35 {
		t.Fatal(len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if strings.Contains(tool.Name, "prepare") || !tool.Annotations.ReadOnlyHint {
			t.Fatal("write registered")
		}
	}
	for _, name := range []string{"tossinvest_get_server_status", "tossinvest_get_accounts"} {
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if e != nil || r.IsError {
			t.Fatal(name, e, r)
		}
	}
	grant(t, s)
	r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "tossinvest_get_holdings", Arguments: map[string]any{"account_alias": "primary"}})
	if e != nil || r.IsError {
		t.Fatal(e, r)
	}
	if _, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "tossinvest_prepare_order", Arguments: map[string]any{"approved": true}}); e == nil {
		t.Fatal("unknown tool not protocol error")
	}
	for _, tc := range []struct {
		path, host, origin, token string
		want                      int
	}{{"/healthz", "", "", "", 200}, {"/admin/v1/status", "", "", "", 401}, {"/mcp", "", "", "wrong", 401}, {"/healthz", "evil.invalid", "", "", 403}, {"/healthz", "", "http://evil.invalid", "", 403}, {"/owner/v1/status", "", "", "", 404}} {
		req, _ := http.NewRequest("GET", s.URL()+tc.path, nil)
		if tc.host != "" {
			req.Host = tc.host
		}
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		if tc.token != "" {
			req.Header.Set("Authorization", tc.token)
		}
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s got %d want %d", tc.path, resp.StatusCode, tc.want)
		}
	}
}
func TestMockCredentialLifecycleSingleInstanceAndMode(t *testing.T) {
	s := startTest(t, "R1")
	ctx := context.Background()
	c, e := ConnectionCredential(ctx, s.Options.Root, "mock")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(s.Options.Root, "mock", "connection.json")
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = ConnectionCredential(ctx, s.Options.Root, "mock"); e == nil {
		t.Fatal("unsafe file accepted")
	}
	os.Chmod(path, 0600)
	if _, e = Start(ctx, s.Options); e == nil {
		t.Fatal("duplicate instance")
	}
	for _, backend := range []string{"mock", "toss"} {
		if _, e = Start(ctx, Options{Root: s.Options.Root, Backend: backend, Mode: "trading"}); e == nil {
			t.Fatal("trading started")
		}
	}
	s.Close()
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("credential file retained after shutdown")
	}
	next, e := Start(ctx, s.Options)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	newCredential, e := ConnectionCredential(ctx, s.Options.Root, "mock")
	if e != nil {
		t.Fatal(e)
	}
	if newCredential.Credential == c.Credential || newCredential.RunID == c.RunID {
		t.Fatal("credential not rotated")
	}
	req, _ := http.NewRequest("GET", next.URL()+"/admin/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+c.Credential)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("old token accepted")
	}
	os.Rename(path, path+".real")
	os.Symlink(path+".real", path)
	if _, e = ConnectionCredential(ctx, s.Options.Root, "mock"); e == nil {
		t.Fatal("symlink accepted")
	}
	os.Remove(path)
	os.Rename(path+".real", path)
}
func TestR2StreamCallsAndRecovery(t *testing.T) {
	s := startTest(t, "R2")
	grant(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	session, e := mcp.NewClient(&mcp.Implementation{Name: "R2-contract-client", Version: "1.8.0"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: s.URL() + "/mcp", HTTPClient: s.ClientHTTP()}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil || len(tools.Tools) != 39 {
		t.Fatal(e, len(tools.Tools))
	}
	subs := []any{}
	for _, ch := range []string{"trade:kr", "trade:us", "orderbook:kr", "orderbook:us"} {
		symbol := "AAPL"
		if strings.HasSuffix(ch, "kr") {
			symbol = "005930"
		}
		subs = append(subs, map[string]any{"channel": ch, "codes": []string{symbol}})
	}
	r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "tossinvest_set_subscriptions", Arguments: map[string]any{"subscriptions": subs}})
	if e != nil || r.IsError {
		t.Fatal(e, r)
	}
	deadline := time.Now().Add(20 * time.Second)
	var state contract.Object
	for time.Now().Before(deadline) {
		state, e = s.Service.Status(core.DefaultPrincipal())
		if e != nil {
			t.Fatal(e)
		}
		if state["streams_ready"] == true {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if state["streams_ready"] != true {
		b, _ := json.Marshal(state)
		t.Fatalf("streams not ready: %s", b)
	}
	for _, ch := range []string{"trade:kr", "trade:us", "orderbook:kr", "orderbook:us"} {
		symbol := "AAPL"
		if strings.HasSuffix(ch, "kr") {
			symbol = "005930"
		}
		r, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "tossinvest_get_stream_snapshot", Arguments: map[string]any{"channel": ch, "symbol": symbol}})
		if e != nil || r.IsError {
			t.Fatal(ch, e, r)
		}
	}
	r, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "tossinvest_list_order_events", Arguments: map[string]any{"account_alias": "primary"}})
	if e != nil || r.IsError {
		t.Fatal(e, r)
	}
	b, _ := json.Marshal(r.StructuredContent)
	if strings.Contains(string(b), "accountSeq") || !strings.Contains(string(b), "FILL") {
		t.Fatalf("event masking/commit: %s", b)
	}
	r, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "tossinvest_set_subscriptions", Arguments: map[string]any{"subscriptions": []any{}}})
	if e != nil || r.IsError {
		t.Fatal("empty full replacement", e, r)
	}
	deadline = time.Now().Add(5 * time.Second)
	cleared := false
	for time.Now().Before(deadline) {
		s.Service.Mu.Lock()
		state := s.Service.Streams.StatusLocked(core.DefaultPrincipal())
		s.Service.Mu.Unlock()
		active := state["active_subscriptions"].([]string)
		if len(active) == 1 && active[0] == "personal:order:primary" {
			cleared = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !cleared {
		t.Fatal("empty watchlist did not clear markets while retaining mandatory orders")
	}
	r, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "tossinvest_get_stream_snapshot", Arguments: map[string]any{"channel": "trade:us", "symbol": "AAPL"}})
	if e != nil || !r.IsError {
		t.Fatal("removed watchlist snapshot still accessible")
	}

}

func TestBackendIsolationWithFakeSecretsNoKeychain(t *testing.T) {
	app := root(t)
	ctx := context.Background()
	c := contract.Load()
	m := mock.New(c)
	defer m.Close()
	tossToken := core.Random()
	deps := dependencies{credential: func() (string, error) { return tossToken, nil }, broker: func(*contract.Catalog) *toss.REST { return m.REST }}
	for _, backend := range []string{"mock", "toss"} {
		if _, e := start(ctx, Options{Root: app, Backend: backend, Mode: "trading"}, deps); e == nil {
			t.Fatal("trading not denied")
		}
	}
	real, e := start(ctx, Options{Root: app, Backend: "toss", Listen: address(t), Milestone: "R1"}, deps)
	if e != nil {
		t.Fatal(e)
	}
	real.Close()
	forbidden := dependencies{credential: func() (string, error) { t.Fatal("mock read real connection provider"); return "", nil }, broker: func(*contract.Catalog) *toss.REST { t.Fatal("mock invoked real broker factory"); return nil }}
	mocked, e := start(ctx, Options{Root: app, Backend: "mock", Listen: address(t), Milestone: "R1"}, forbidden)
	if e != nil {
		t.Fatal(e)
	}
	mockToken := mocked.credential
	req, _ := http.NewRequest("GET", mocked.URL()+"/admin/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+tossToken)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("Toss credential accepted by mock")
	}
	mocked.Close()
	real, e = start(ctx, Options{Root: app, Backend: "toss", Listen: address(t), Milestone: "R1"}, deps)
	if e != nil {
		t.Fatal(e)
	}
	defer real.Close()
	req, _ = http.NewRequest("GET", real.URL()+"/admin/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+mockToken)
	resp, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("mock credential accepted by Toss")
	}
	for _, path := range []string{"state.sqlite3", "mock/state.sqlite3"} {
		if _, e = os.Stat(filepath.Join(app, path)); e != nil {
			t.Fatal(e)
		}
	}
}
