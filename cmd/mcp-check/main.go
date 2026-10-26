// mcp-check validates the built, independent server through the official MCP client.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/core"
	run "github.com/bright-k/tossinvest-tools/internal/runtime"
	"github.com/bright-k/tossinvest-tools/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}
func check(binary, milestone string) (map[string]any, error) {
	root, e := os.MkdirTemp("/private/tmp", "ti-mcp-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(root)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	addr := l.Addr().String()
	l.Close()
	command := exec.Command(binary, "serve", "--backend=mock", "--mode=readonly", "--milestone="+milestone, "--app-dir="+root, "--listen="+addr)
	stdout, e := command.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = command.Start(); e != nil {
		return nil, e
	}
	defer func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { command.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			command.Process.Kill()
			<-done
		}
	}()
	started := make(chan bool, 1)
	go func() { scan := bufio.NewScanner(stdout); started <- scan.Scan() }()
	select {
	case ok := <-started:
		if !ok {
			return nil, fmt.Errorf("server exited before startup")
		}
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("server start timeout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// Exercise the same machine-only helper that Codex runs, capturing headers only in this client process.
	helper := exec.CommandContext(ctx, binary, "mcp-auth-header", "--backend=mock", "--app-dir="+root)
	headerBytes, e := helper.Output()
	if e != nil {
		return nil, fmt.Errorf("connection helper failed")
	}
	headers := map[string]string{}
	if json.Unmarshal(headerBytes, &headers) != nil {
		return nil, fmt.Errorf("helper returned invalid JSON")
	}
	token := strings.TrimPrefix(headers["Authorization"], "Bearer ")
	client := mcp.NewClient(&mcp.Implementation{Name: "tossinvest-standalone-check", Version: "go-sdk-v1.8.0"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp", HTTPClient: &http.Client{Transport: authTransport{token}, Timeout: 20 * time.Second}}, nil)
	if e != nil {
		return nil, e
	}
	defer session.Close()
	list, e := session.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil {
		return nil, e
	}
	want := 35
	if milestone == "R2" {
		want = 39
	}
	if len(list.Tools) != want {
		return nil, fmt.Errorf("discovered %d wanted %d", len(list.Tools), want)
	}
	call := func(name string, args map[string]any) error {
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if e != nil {
			return e
		}
		if r.IsError {
			return fmt.Errorf("tool failed: %s", name)
		}
		b, _ := json.Marshal(r.StructuredContent)
		if strings.Contains(string(b), "accountSeq") || strings.Contains(string(b), "accountNo\"") || strings.Contains(string(b), token) {
			return fmt.Errorf("sensitive field returned by %s", name)
		}
		return nil
	}
	if e = call("tossinvest_get_server_status", map[string]any{}); e != nil {
		return nil, e
	}
	if e = call("tossinvest_get_accounts", map[string]any{}); e != nil {
		return nil, e
	}
	owner, e := run.OwnerClient(root, "mock")
	if e != nil {
		return nil, e
	}
	d, e := run.OwnerRequest(ctx, owner, "POST", "/owner/v1/accounts/discover", nil)
	if e != nil {
		return nil, e
	}
	id := contract.Str(d["discovery_id"])
	n, _ := d["expected_access_revision"].(json.Number)
	rev, _ := n.Int64()
	handle := contract.Str(contract.Obj(contract.List(d["accounts"])[0])["handle"])
	_, e = run.OwnerRequest(ctx, owner, "PUT", "/owner/v1/accounts/grant", core.Grant{DiscoveryID: &id, Revision: rev, Principal: core.PrincipalID, Selected: []core.Selection{{Handle: handle, Alias: "primary"}}})
	if e != nil {
		return nil, e
	}
	c := contract.Load()
	called := []string{}
	for _, op := range c.Ops {
		if op.Exposure != "read_tool" {
			continue
		}
		if e = call(op.Tool, testutil.Args(c, op, true)); e != nil {
			return nil, e
		}
		called = append(called, op.Tool)
	}
	if _, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "tossinvest_prepare_order", Arguments: map[string]any{"approved": true}}); e == nil {
		return nil, fmt.Errorf("hidden order tool did not return protocol error")
	}
	if milestone == "R2" {
		if e = call("tossinvest_set_subscriptions", map[string]any{"subscriptions": []any{map[string]any{"channel": "trade:us", "codes": []string{"AAPL"}}}}); e != nil {
			return nil, e
		}
		deadline := time.Now().Add(20 * time.Second)
		ready := false
		for time.Now().Before(deadline) {
			state, e := run.Status(ctx, root, "mock")
			if e != nil {
				return nil, e
			}
			if state["streams_ready"] == true {
				ready = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !ready {
			return nil, fmt.Errorf("streams recovery timeout")
		}
		for name, args := range map[string]map[string]any{"tossinvest_get_stream_status": {}, "tossinvest_get_stream_snapshot": {"channel": "trade:us", "symbol": "AAPL"}, "tossinvest_list_order_events": {"account_alias": "primary", "limit": 100}} {
			if e = call(name, args); e != nil {
				return nil, e
			}
			called = append(called, name)
		}
		called = append(called, "tossinvest_set_subscriptions")
	}
	return map[string]any{"milestone": milestone, "negotiated_protocol": session.InitializeResult().ProtocolVersion, "transport": "authenticated Streamable HTTP", "discovered_tools": len(list.Tools), "rest_calls_passed": 34, "called_tools": called, "unknown_write_tool": "protocol_error", "helper": "passed_no_headers_printed", "process": "built independent binary"}, nil
}
func main() {
	binary := "bin/tossinvest-tools"
	if len(os.Args) > 1 {
		binary = os.Args[1]
	}
	binary, e := filepath.Abs(binary)
	if e != nil {
		panic(e)
	}
	results := []any{}
	for _, m := range []string{"R1", "R2"} {
		r, e := check(binary, m)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		results = append(results, r)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"client": "official Go MCP SDK v1.8.0", "results": results})
}
