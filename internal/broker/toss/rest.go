package toss

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	g "github.com/bright-k/tossinvest-tools/internal/broker/toss/generated"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/fault"
)

type dispatchKey struct{}

func WithDispatchGuard(ctx context.Context, guard func() error) context.Context {
	return context.WithValue(ctx, dispatchKey{}, guard)
}
func checkDispatch(ctx context.Context) error {
	if guard, ok := ctx.Value(dispatchKey{}).(func() error); ok {
		return guard()
	}
	return nil
}

const Origin = "https://openapi.tossinvest.com"
const WSURL = "wss://openapi-ws.tossinvest.com/ws/v1"

type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}
type SecretStore interface {
	Read(context.Context) (Credentials, error)
}
type Broker interface {
	Query(context.Context, contract.Operation, map[string]any, string) (any, error)
	Identity() string
	Ready() bool
}

// Guard classifies the actual origin, method and path. Caller labels are never trusted.
type Guard struct {
	Base    string
	Next    http.RoundTripper
	Catalog *contract.Catalog
}

func (gd *Guard) RoundTrip(r *http.Request) (*http.Response, error) {
	base, _ := url.Parse(gd.Base)
	if r.URL.Scheme != base.Scheme || r.URL.Host != base.Host || r.URL.User != nil || r.URL.Fragment != "" || r.URL.Opaque != "" {
		return nil, fault.New("READONLY_VIOLATION", "upstream origin denied")
	}
	if r.Method == "POST" && r.URL.EscapedPath() == "/oauth2/token" && r.URL.RawQuery == "" {
		return gd.Next.RoundTrip(r)
	}
	if r.Method != "GET" {
		return nil, fault.New("READONLY_VIOLATION", "broker write denied")
	}
	p := r.URL.EscapedPath()
	if strings.Contains(strings.ToLower(p), "%2f") || strings.Contains(p, "..") || strings.Contains(p, "//") {
		return nil, fault.New("READONLY_VIOLATION", "ambiguous path denied")
	}
	for _, o := range gd.Catalog.Ops {
		if o.Method != "GET" {
			continue
		}
		a, b := strings.Split(p, "/"), strings.Split(o.Path, "/")
		if len(a) != len(b) {
			continue
		}
		match := true
		for i, s := range b {
			if strings.HasPrefix(s, "{") {
				if a[i] == "" {
					match = false
				}
			} else if a[i] != s {
				match = false
			}
		}
		if match {
			return gd.Next.RoundTrip(r)
		}
	}
	return nil, fault.New("READONLY_VIOLATION", "unregistered upstream operation")
}

type REST struct {
	catalog   *contract.Catalog
	base      string
	HTTP      *http.Client
	secrets   SecretStore
	tokenMu   sync.Mutex
	token     string
	expiry    time.Time
	identity  string
	rateMu    sync.Mutex
	next      map[string]time.Time
	intervals map[string]time.Duration
	ready     bool
}

func NewREST(c *contract.Catalog, secrets SecretStore) *REST {
	return NewRESTForTransport(c, secrets, Origin, http.DefaultTransport)
}

// NewRESTForTransport permits contract tests to inject a local server; no CLI override exists.
func NewRESTForTransport(c *contract.Catalog, secrets SecretStore, base string, rt http.RoundTripper) *REST {
	return &REST{catalog: c, base: base, secrets: secrets, next: map[string]time.Time{}, intervals: map[string]time.Duration{}, HTTP: &http.Client{Timeout: 15 * time.Second, Transport: &Guard{Base: base, Next: rt, Catalog: c}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect denied") }}}
}
func (r *REST) Identity() string { r.tokenMu.Lock(); defer r.tokenMu.Unlock(); return r.identity }
func (r *REST) Ready() bool      { r.tokenMu.Lock(); defer r.tokenMu.Unlock(); return r.ready }
func (r *REST) Token(ctx context.Context, rejected string) (string, error) {
	r.tokenMu.Lock()
	defer r.tokenMu.Unlock()
	if r.token != "" && time.Now().Add(30*time.Second).Before(r.expiry) && (rejected == "" || r.token != rejected) {
		return r.token, nil
	}
	cred, err := r.secrets.Read(ctx)
	if err != nil {
		r.ready = false
		return "", fault.New("NOT_READY", "Toss credentials unavailable; run local setup or unlock Keychain")
	}
	h := sha256.Sum256([]byte(cred.ClientID))
	identity := hex.EncodeToString(h[:])
	if r.identity != "" && r.identity != identity {
		r.token = ""
	}
	r.identity = identity
	req, err := g.NewIssueOAuth2TokenRequestWithFormdataBody(r.base, g.IssueOAuth2TokenFormdataRequestBody{GrantType: g.ClientCredentials, ClientId: cred.ClientID, ClientSecret: cred.ClientSecret})
	if err != nil {
		return "", fault.New("CONFIG_INVALID", "token request invalid")
	}
	req = req.WithContext(ctx)
	if err = r.pace(ctx, "AUTH"); err != nil {
		return "", fault.New("UPSTREAM_ERROR", "token request cancelled")
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		r.ready = false
		return "", fault.New("UPSTREAM_ERROR", "token connection failed")
	}
	defer resp.Body.Close()
	r.observe("AUTH", resp.Header, resp.StatusCode)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fault.New("UPSTREAM_ERROR", "token read failed")
	}
	if resp.StatusCode != 200 {
		r.ready = false
		return "", upstreamError(resp, body, true)
	}
	var tok g.OAuth2TokenResponse
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" || tok.ExpiresIn <= 30 || tok.TokenType != g.Bearer {
		r.ready = false
		return "", fault.New("UPSTREAM_DATA_INVALID", "invalid token response")
	}
	r.token = tok.AccessToken
	r.expiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	r.ready = true
	return r.token, nil
}
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func defaultInterval(group string) time.Duration {
	switch group {
	case "ACCOUNT", "STOCK_ALL":
		return time.Second
	case "AUTH":
		return 200 * time.Millisecond
	case "MARKET_INFO", "ORDER_INFO":
		return 334 * time.Millisecond
	default:
		return 250 * time.Millisecond
	}
}
func (r *REST) pace(ctx context.Context, group string) error {
	for {
		r.rateMu.Lock()
		now := time.Now()
		at := r.next[group]
		interval := defaultInterval(group)
		if r.intervals[group] > interval {
			interval = r.intervals[group]
		}
		if !at.After(now) {
			r.next[group] = now.Add(interval)
			r.rateMu.Unlock()
			return nil
		}
		r.rateMu.Unlock()
		if e := wait(ctx, time.Until(at)); e != nil {
			return e
		}
	}
}
func (r *REST) observe(group string, h http.Header, status int) {
	var until time.Time
	if ra := h.Get("Retry-After"); ra != "" {
		if d, e := time.ParseDuration(ra + "s"); e == nil && d >= 0 {
			until = time.Now().Add(d)
		} else {
			until, _ = http.ParseTime(ra)
		}
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		if d, e := time.ParseDuration(h.Get("X-RateLimit-Reset") + "s"); e == nil && d >= 0 {
			reset := time.Now().Add(d)
			if reset.After(until) {
				until = reset
			}
		}
	}
	if status == 429 && until.IsZero() {
		until = time.Now().Add(time.Second)
	}
	r.rateMu.Lock()
	defer r.rateMu.Unlock()
	if n, e := strconv.ParseInt(h.Get("X-RateLimit-Limit"), 10, 64); e == nil && n > 0 && n < 1000000 {
		r.intervals[group] = time.Second / time.Duration(n)
		if at := time.Now().Add(r.intervals[group]); at.After(r.next[group]) {
			r.next[group] = at
		}
	}
	if until.After(r.next[group]) {
		r.next[group] = until
	}
}
func (r *REST) Query(ctx context.Context, op contract.Operation, args map[string]any, account string) (any, error) {
	if op.Method != "GET" || op.Exposure != "read_tool" {
		return nil, fault.New("READONLY_VIOLATION", "readonly service denied write")
	}
	if err := r.catalog.Input(op, args); err != nil {
		return nil, fault.New("INVALID_ARGUMENT", err.Error())
	}
	token, err := r.Token(ctx, "")
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := r.pace(ctx, rateGroup(op)); err != nil {
			return nil, fault.New("UPSTREAM_ERROR", "request cancelled")
		}
		req, err := BuildRequest(op.ID, r.base, args, account)
		if err != nil {
			return nil, fault.New("INVALID_ARGUMENT", "request serialization failed")
		}
		req = req.WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if err = checkDispatch(ctx); err != nil {
			return nil, err
		}
		resp, err := r.HTTP.Do(req)
		if err != nil {
			if errors.As(err, new(*fault.Error)) {
				return nil, err
			}
			return nil, fault.New("UPSTREAM_ERROR", "upstream connection failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
		resp.Body.Close()
		r.observe(rateGroup(op), resp.Header, resp.StatusCode)
		if readErr != nil || len(body) > 16<<20 {
			return nil, fault.New("UPSTREAM_DATA_INVALID", "upstream response too large or incomplete")
		}
		if resp.StatusCode == 401 && attempt == 0 {
			token, err = r.Token(ctx, token)
			if err != nil {
				return nil, err
			}
			continue
		}
		if resp.StatusCode != 200 {
			return nil, upstreamError(resp, body, false)
		}
		var data contract.Object
		if contract.Decode(body, &data) != nil || r.catalog.Validate(r.catalog.ResponseSchema(op, "200"), data, false) != nil {
			return nil, fault.New("UPSTREAM_DATA_INVALID", "response schema invalid")
		}
		if DecodeTyped(op.ID, resp.StatusCode, body) != nil {
			return nil, fault.New("UPSTREAM_DATA_INVALID", "typed response decode failed")
		}
		return data["result"], nil
	}
	return nil, fault.New("UPSTREAM_ERROR", "authentication rejected after refresh")
}
func rateGroup(o contract.Operation) string {
	if o.RateGroup != "" {
		return o.RateGroup
	}
	return o.Group
}
func upstreamError(resp *http.Response, b []byte, oauth bool) *fault.Error {
	code := "UPSTREAM_ERROR"
	switch resp.StatusCode {
	case 404:
		code = "RESOURCE_NOT_FOUND"
	case 429:
		code = "UPSTREAM_RATE_LIMITED"
	}
	e := fault.New(code, "upstream request rejected")
	e.Status = resp.StatusCode
	e.Retryable = resp.StatusCode == 429 || resp.StatusCode >= 500
	e.RetryAfter = resp.Header.Get("Retry-After")
	e.RequestID = resp.Header.Get("X-Request-Id")
	var m contract.Object
	_ = contract.Decode(b, &m)
	if oauth {
		e.UpstreamCode = contract.Str(m["error"])
	} else {
		x := contract.Obj(m["error"])
		e.UpstreamCode = contract.Str(x["code"])
		if e.RequestID == "" {
			e.RequestID = contract.Str(x["requestId"])
		}
		e.Data = safeHints(contract.Obj(x["data"]))
	}
	return e
}

// Preserve future error hints while removing credential and raw account fields.
func safeHints(m contract.Object) contract.Object {
	out := contract.Object{}
	for k, v := range m {
		normalized := strings.ToLower(strings.ReplaceAll(k, "_", ""))
		if strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") || normalized == "authorization" || normalized == "accountseq" || normalized == "accountno" || normalized == "clientid" {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			out[k] = safeHints(x)
		case []any:
			a := []any{}
			for _, item := range x {
				if obj, ok := item.(map[string]any); ok {
					a = append(a, safeHints(obj))
				} else {
					a = append(a, item)
				}
			}
			out[k] = a
		default:
			out[k] = v
		}
	}
	return out
}
