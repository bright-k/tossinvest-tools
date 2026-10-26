package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type queryRoundTripper func(*http.Request) (*http.Response, error)

func (f queryRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestQueryClientUsesServerAccountHeaderAndFilters(t *testing.T) {
	s := startTest(t, "R1")
	grant(t, s)
	broker := s.Service.Broker.(*toss.REST)
	next := broker.HTTP.Transport
	var captured atomic.Bool
	broker.HTTP.Transport = queryRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/holdings" {
			if r.Method != "GET" || r.URL.Query().Get("symbol") != "005930" || r.Header.Get("X-Tossinvest-Account") != "1" {
				t.Error("server request did not preserve filter/derived account")
			}
			captured.Store(true)
		}
		return next.RoundTrip(r)
	})
	result, err := Query(context.Background(), s.Options.Root, "mock", "tossinvest_get_holdings", contract.Object{"account_alias": "primary", "symbol": "005930"})
	if err != nil {
		t.Fatal(err)
	}
	if !captured.Load() || contract.Obj(result["meta"])["account_alias"] != "primary" {
		t.Fatal("query bypassed server broker")
	}
	// A server-owned upstream error retains its structured details through MCP.
	broker.HTTP.Transport = queryRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/prices" {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"requestId":"mock-query-request","code":"mock-rate-limited","message":"ignored raw message","data":{"futureCount":9007199254740993}}}`))}, nil
		}
		return next.RoundTrip(r)
	})
	_, err = Query(context.Background(), s.Options.Root, "mock", "tossinvest_get_prices", contract.Object{"symbols": "AAPL"})
	e := fault.Wrap(err)
	if e.Code != "UPSTREAM_RATE_LIMITED" || !e.Retryable || e.Status != 429 || e.RequestID != "mock-query-request" || e.UpstreamCode != "mock-rate-limited" || e.RetryAfter != "1" || contract.Obj(e.Data)["futureCount"] != json.Number("9007199254740993") {
		t.Fatal("server error lost its details")
	}
}

func TestQueryTransportKeepsCredentialsOnLocalOrigin(t *testing.T) {
	var calls int
	transport := &queryTransport{origin: "http://127.0.0.1:17840", token: "mock-private-connection", next: queryRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer mock-private-connection" {
			t.Error("missing authentication")
		}
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("private diagnostic"))}, nil
	})}
	for _, url := range []string{"https://127.0.0.1:17840/mcp", "http://example.invalid/mcp", "http://user@127.0.0.1:17840/mcp"} {
		req, _ := http.NewRequest("POST", url, nil)
		_, err := transport.RoundTrip(req)
		if fault.Wrap(err).Code != "PERMISSION_DENIED" {
			t.Fatal("nonlocal origin accepted")
		}
	}
	if calls != 0 {
		t.Fatal("credential reached a denied origin")
	}
	request, _ := http.NewRequest("POST", "http://127.0.0.1:17840/mcp", nil)
	_, err := transport.RoundTrip(request)
	if fault.Wrap(err).Code != "AUTH_REQUIRED" || request.Header.Get("Authorization") != "" {
		t.Fatal("HTTP authentication handling or request mutation")
	}
}

func TestQueryResultPreservesCanonicalJSONAndRejectsMalformedResponses(t *testing.T) {
	body := `{"data":{"price":"123456789012345678901234.000001","quantity":"0.000000001","futureInteger":9007199254740993,"unknown":null},"meta":{"schema_version":1,"mode":"readonly","backend":"mock","fetched_at":"2026-10-26T00:00:00Z","data_timestamp":null,"stale":null}}`
	makeResult := func(text string) *mcp.CallToolResult {
		return &mcp.CallToolResult{StructuredContent: map[string]any{}, Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	}
	result, err := decodeQueryResult(makeResult(body), "mock")
	if err != nil {
		t.Fatal(err)
	}
	data := contract.Obj(result["data"])
	if data["price"] != "123456789012345678901234.000001" || data["quantity"] != "0.000000001" || data["futureInteger"] != json.Number("9007199254740993") || data["unknown"] != nil {
		t.Fatal("JSON precision/null changed")
	}
	for _, malformed := range []*mcp.CallToolResult{nil, {}, makeResult(`{`), makeResult(body + `{}`), makeResult(`{"data":[]}`), makeResult(strings.Replace(body, `"backend":"mock"`, `"backend":"toss"`, 1)), {IsError: true, StructuredContent: map[string]any{}, Content: []mcp.Content{&mcp.TextContent{Text: `{"error":{}}`}}}} {
		if _, err = decodeQueryResult(malformed, "mock"); err == nil || fault.Wrap(err).Code != "UPSTREAM_DATA_INVALID" {
			t.Fatal("invalid response became successful empty data")
		}
	}
	_, err = Query(context.Background(), t.TempDir(), "mock", "tossinvest_prepare_order", contract.Object{})
	if fault.Wrap(err).Code != "READONLY_VIOLATION" {
		t.Fatal("CLI client accepted a non-read tool")
	}
}
