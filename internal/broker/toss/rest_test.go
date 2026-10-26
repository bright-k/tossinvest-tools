package toss_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bright-k/tossinvest-tools/internal/broker/mock"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	g "github.com/bright-k/tossinvest-tools/internal/broker/toss/generated"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/testutil"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type secret struct{}

func (secret) Read(context.Context) (toss.Credentials, error) {
	return toss.Credentials{ClientID: "mock-only-client", ClientSecret: "mock-only-secret"}, nil
}
func TestAll34RESTInputsResponsesPermissionsErrors(t *testing.T) {
	c := contract.Load()
	count := 0
	for _, op := range c.Ops {
		if op.Method != "GET" {
			continue
		}
		op := op
		count++
		t.Run(op.ID, func(t *testing.T) {
			for _, all := range []bool{false, true} {
				m := mock.New(c)
				args := testutil.Args(c, op, all)
				if e := c.Input(op, args); e != nil {
					t.Fatal(e)
				}
				m.Hook = func(req *http.Request) {
					for _, p := range c.Params(op) {
						if p["in"] == "header" {
							if req.Header.Get("X-Tossinvest-Account") != "1" {
								t.Error("missing server injected account")
							}
							continue
						}
						n := contract.Str(p["name"])
						v, exists := args[n]
						if !exists {
							if _, present := req.URL.Query()[n]; present {
								t.Error("unexpected optional")
							}
							continue
						}
						if p["in"] == "query" && req.URL.Query().Get(n) != fmt.Sprint(v) {
							t.Errorf("%s serialization: %s != %v", n, req.URL.Query().Get(n), v)
						}
						if p["in"] == "path" && !strings.Contains(req.URL.Path, fmt.Sprint(v)) {
							t.Error("path serialization")
						}
					}
				}
				data, e := m.REST.Query(context.Background(), op, args, "1")
				if e != nil {
					t.Fatal(e)
				}
				if data == nil {
					t.Fatal("nil data")
				}
				m.Close()
			}
			for _, status := range []int{400, 401, 403, 404, 429, 500} {
				m := mock.New(c)
				m.Status[op.ID] = status
				_, e := m.REST.Query(context.Background(), op, testutil.Args(c, op, false), "1")
				if e == nil {
					t.Fatalf("status %d became success", status)
				}
				m.Close()
			}
			if len(c.Params(op)) > 0 {
				bad := testutil.Args(c, op, false)
				bad["__unknown"] = true
				m := mock.New(c)
				_, e := m.REST.Query(context.Background(), op, bad, "1")
				if e == nil || len(m.Calls) != 0 {
					t.Fatal("invalid input reached network")
				}
				m.Close()
			}
			if op.Account {
				m := mock.New(c)
				_, e := m.REST.Query(context.Background(), op, testutil.Args(c, op, false), "999")
				if e == nil {
					t.Fatal("upstream account restriction")
				}
				m.Close()
			}
		})
	}
	if count != 34 {
		t.Fatal(count)
	}
}
func TestReadonlyFinalTransportGuard(t *testing.T) {
	c := contract.Load()
	var sent atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		http.Redirect(w, r, "http://example.invalid/stolen", 302)
	}))
	defer s.Close()
	r := toss.NewRESTForTransport(c, secret{}, s.URL, http.DefaultTransport)
	for _, op := range c.Ops {
		if op.Exposure != "approved_action" {
			continue
		}
		req, _ := http.NewRequest(op.Method, s.URL+strings.ReplaceAll(strings.ReplaceAll(op.Path, "{orderId}", "test"), "{conditionalOrderId}", "test"), strings.NewReader(`{}`))
		req.Header.Set("X-Operation-ID", "getPrices")
		if _, e := r.HTTP.Do(req); e == nil {
			t.Fatal("write reached network")
		}
	}
	for _, target := range []string{"https://example.invalid/api/v1/prices", "http://example.invalid/api/v1/prices", s.URL + "/api/v1/unknown", s.URL + "/api/v1/orders/a%2Fb", s.URL + "/api/v1/orders/../prices"} {
		req, _ := http.NewRequest("GET", target, nil)
		if _, e := r.HTTP.Do(req); e == nil {
			t.Fatal("unsafe request allowed")
		}
	}
	client, err := g.NewClient(s.URL, g.WithHTTPClient(r.HTTP))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	body := func() io.Reader { return strings.NewReader(`{}`) }
	direct := []func() error{
		func() error {
			_, e := client.CreateOrderWithBody(ctx, &g.CreateOrderParams{XTossinvestAccount: 1}, "application/json", body())
			return e
		},
		func() error {
			_, e := client.ModifyOrderWithBody(ctx, "mock", &g.ModifyOrderParams{XTossinvestAccount: 1}, "application/json", body())
			return e
		},
		func() error {
			_, e := client.CancelOrderWithBody(ctx, "mock", &g.CancelOrderParams{XTossinvestAccount: 1}, "application/json", body())
			return e
		},
		func() error {
			_, e := client.CreateConditionalOrderWithBody(ctx, &g.CreateConditionalOrderParams{XTossinvestAccount: 1}, "application/json", body())
			return e
		},
		func() error {
			_, e := client.ModifyConditionalOrderWithBody(ctx, "mock", &g.ModifyConditionalOrderParams{XTossinvestAccount: 1}, "application/json", body())
			return e
		},
		func() error {
			_, e := client.CancelConditionalOrder(ctx, "mock", &g.CancelConditionalOrderParams{XTossinvestAccount: 1})
			return e
		},
	}
	for _, call := range direct {
		if e := call(); e == nil {
			t.Fatal("generated write method reached transport")
		}
	}
	if sent.Load() != 0 {
		t.Fatalf("sent %d denied requests", sent.Load())
	}
	req, _ := http.NewRequest("GET", s.URL+"/api/v1/prices", nil)
	req.Header.Set("Authorization", "Bearer mock-secret")
	if _, e := r.HTTP.Do(req); e == nil {
		t.Fatal("redirect followed")
	}
	if sent.Load() != 1 {
		t.Fatal("redirect escaped")
	}
}
func TestTokenFormSingleflight401(t *testing.T) {
	c := contract.Load()
	var issued atomic.Int32
	var arrived atomic.Int32
	gate := make(chan struct{})
	const n = 12
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/oauth2/token" {
			_ = r.ParseForm()
			if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Form.Get("grant_type") != "client_credentials" {
				t.Error("invalid form")
			}
			i := issued.Add(1)
			fmt.Fprintf(w, `{"access_token":"mock-%d","token_type":"Bearer","expires_in":86400}`, i)
			return
		}
		if r.Header.Get("Authorization") == "Bearer mock-1" {
			if arrived.Add(1) == n {
				close(gate)
			}
			select {
			case <-gate:
			case <-time.After(5 * time.Second):
				t.Error("concurrent requests not all arrived")
			}
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"requestId":"mock","code":"expired","message":"expired"}}`)
			return
		}
		fmt.Fprint(w, `{"result":[{"symbol":"AAPL","timestamp":null,"lastPrice":"123.000001","currency":"NEW_CURRENCY","newField":{"n":"000.10"}}]}`)
	}))
	defer s.Close()
	r := toss.NewRESTForTransport(c, secret{}, s.URL, http.DefaultTransport)
	if _, e := r.Token(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	// Independent groups avoid pacing while sharing the one token manager.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			op, _ := c.Find("getPrices")
			op.RateGroup = fmt.Sprint(i)
			data, e := r.Query(context.Background(), op, contract.Object{"symbols": "AAPL"}, "")
			if e != nil {
				t.Error(e)
				return
			}
			b, _ := json.Marshal(data)
			if !strings.Contains(string(b), "newField") || !strings.Contains(string(b), "NEW_CURRENCY") {
				t.Error("forward-compatible raw fields lost")
			}
		}(i)
	}
	wg.Wait()
	if issued.Load() != 2 {
		t.Fatalf("token refresh count %d", issued.Load())
	}
}
func TestURLDateOffset(t *testing.T) {
	c := contract.Load()
	op, _ := c.Find("getCandles")
	a := testutil.Args(c, op, true)
	a["before"] = "2026-03-25T09:00:00+09:00"
	r, e := toss.BuildRequest(op.ID, toss.Origin, a, "")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(r.URL.RawQuery, "%2B09%3A00") {
		t.Fatal(r.URL.RawQuery)
	}
	if _, e = io.ReadAll(strings.NewReader("")); e != nil {
		t.Fatal(e)
	}
}
