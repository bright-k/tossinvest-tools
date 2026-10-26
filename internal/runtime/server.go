package runtime

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/bright-k/tossinvest-tools/internal/broker/mock"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/core"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/bright-k/tossinvest-tools/internal/platform"
	"github.com/bright-k/tossinvest-tools/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const Version = "0.2.0"

type Options struct{ Root, Backend, Listen, Mode, Milestone string }
type Connection struct {
	Backend    string `json:"backend"`
	RunID      string `json:"run_id"`
	Listen     string `json:"listen"`
	Credential string `json:"credential"`
}
type Server struct {
	Service     *core.Service
	Options     Options
	HTTP, Owner *http.Server
	Listener    net.Listener
	Unix        net.Listener
	lock        *platform.Lock
	mock        *mock.Server
	credential  string
	close       sync.Once
	Errors      chan error
	Cancel      context.CancelFunc
	wg          sync.WaitGroup
}

type dependencies struct {
	credential func() (string, error)
	broker     func(*contract.Catalog) *toss.REST
}

func Start(parent context.Context, o Options) (*Server, error) {
	return start(parent, o, dependencies{})
}
func start(parent context.Context, o Options, deps dependencies) (srv *Server, err error) {
	if o.Mode == "" {
		o.Mode = "readonly"
	}
	if o.Mode != "readonly" {
		if o.Mode == "trading" {
			return nil, fault.New("CAPABILITY_UNAVAILABLE", "trading module is not implemented in readonly R2")
		}
		return nil, fault.New("CONFIG_INVALID", "mode must be readonly")
	}
	if o.Root == "" {
		o.Root = DefaultRoot()
	}
	cfg, err := Load(o.Root)
	if err != nil {
		return nil, err
	}
	if o.Backend == "" {
		o.Backend = cfg.Backend
	}
	if o.Listen == "" {
		o.Listen = cfg.Listen
	}
	if o.Milestone == "" {
		o.Milestone = "R2"
	}
	if o.Backend != "toss" && o.Backend != "mock" || o.Milestone != "R1" && o.Milestone != "R2" || platform.ValidateListen(o.Listen) != nil {
		return nil, fault.New("CONFIG_INVALID", "invalid backend, listen, or milestone")
	}
	if err = platform.PrivateDir(o.Root); err != nil {
		return nil, fault.New("CONFIG_INVALID", err.Error())
	}
	dir := BackendDir(o.Root, o.Backend)
	if err = platform.PrivateDir(dir); err != nil {
		return nil, err
	}
	lock, err := platform.Acquire(filepath.Join(o.Root, "server.lock"))
	if err != nil {
		return nil, fault.New("CONFIG_CONFLICT", err.Error())
	}
	srv = &Server{Options: o, lock: lock, Errors: make(chan error, 2)}
	cleanup := srv
	defer func() {
		if err != nil {
			cleanup.Close()
		}
	}()
	db, err := store.Open(filepath.Join(dir, "state.sqlite3"), o.Backend)
	if err != nil {
		return nil, fault.New("NOT_READY", "database open or migration failed")
	}
	catalog := contract.Load()
	var broker toss.Broker
	if o.Backend == "mock" {
		srv.mock = mock.New(catalog)
		broker = srv.mock.REST
		srv.credential = core.Random()
		_, err = srv.mock.REST.Token(parent, "")
		if err != nil {
			db.Close()
			return nil, err
		}
	} else {
		r := toss.NewREST(catalog, platform.TossSecrets{})
		if deps.broker != nil {
			r = deps.broker(catalog)
		}
		broker = r
		credential := platform.ReadMCP
		if deps.credential != nil {
			credential = deps.credential
		}
		srv.credential, err = credential()
		if err != nil || len(srv.credential) < 40 {
			db.Close()
			return nil, fault.New("NOT_READY", "MCP credential unavailable; run local setup")
		}
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		_, _ = r.Token(ctx, "")
		cancel()
	}
	srv.Service, err = core.New(catalog, broker, db, o.Backend, o.Milestone)
	if err != nil {
		db.Close()
		return nil, fault.New("NOT_READY", "service metadata initialization failed")
	}
	hash := sha256.Sum256([]byte(srv.credential))
	if _, err = db.DB.Exec("INSERT INTO metadata VALUES('credential_hash',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", hex.EncodeToString(hash[:])); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	srv.Cancel = cancel
	// Streams are attached only in R2; the same REST service remains usable independently.
	if o.Milestone == "R2" {
		if err = attachStreams(ctx, srv); err != nil {
			return nil, err
		}
	}
	socket := filepath.Join(dir, "owner.sock")
	if len(socket) > 103 {
		return nil, fault.New("CONFIG_INVALID", "owner socket path too long; use shorter app root")
	}
	if st, e := os.Lstat(socket); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, fault.New("CONFIG_INVALID", "owner socket path is not a socket")
		}
		if err = os.Remove(socket); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	unixListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		return nil, err
	}
	srv.Unix = &peerListener{UnixListener: unixListener}
	if err = os.Chmod(socket, 0600); err != nil {
		return nil, err
	}
	srv.Owner = &http.Server{Handler: srv.ownerHandler(), ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	listener, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return nil, fault.New("CONFIG_CONFLICT", "HTTP listen failed")
	}
	srv.Listener = listener
	srv.HTTP = &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16384}
	if o.Backend == "mock" {
		if err = platform.AtomicJSON(filepath.Join(dir, "connection.json"), Connection{o.Backend, srv.Service.RunID, o.Listen, srv.credential}); err != nil {
			return nil, err
		}
	}
	for _, pair := range []struct {
		h *http.Server
		l net.Listener
	}{{srv.Owner, srv.Unix}, {srv.HTTP, srv.Listener}} {
		srv.wg.Add(1)
		go func(h *http.Server, l net.Listener) {
			defer srv.wg.Done()
			if e := h.Serve(l); e != nil && !errors.Is(e, http.ErrServerClosed) {
				select {
				case srv.Errors <- e:
				default:
				}
			}
		}(pair.h, pair.l)
	}
	return srv, nil
}
func (s *Server) Close() {
	s.close.Do(func() {
		if s.Cancel != nil {
			s.Cancel()
		}
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		if s.HTTP != nil {
			_ = s.HTTP.Shutdown(ctx)
		}
		if s.Owner != nil {
			_ = s.Owner.Shutdown(ctx)
		}
		if s.Listener != nil {
			_ = s.Listener.Close()
		}
		if s.Unix != nil {
			_ = s.Unix.Close()
		}
		s.wg.Wait()
		if s.Service != nil {
			closeStreams(s)
			_ = s.Service.Store.Close()
		}
		if s.mock != nil {
			s.mock.Close()
		}
		if s.Options.Backend == "mock" && s.Service != nil {
			_ = os.Remove(filepath.Join(BackendDir(s.Options.Root, "mock"), "connection.json"))
		}
		if s.lock != nil {
			s.lock.Close()
		}
	})
}
func (s *Server) Handler() http.Handler {
	sdk := mcp.NewServer(&mcp.Implementation{Name: "TossInvest Tools Server", Version: Version}, &mcp.ServerOptions{Instructions: "Readonly TossInvest. All queries require local connection authentication and explicit account_alias for account data. Select allowed accounts locally with setup accounts. Order history covers official API supported types only; gap_detected and history_truncated describe observation limits. No trading tools are available."})
	for _, op := range s.Service.Catalog.Ops {
		if op.Exposure != "read_tool" {
			continue
		}
		op := op
		sdk.AddTool(&mcp.Tool{Name: op.Tool, Description: op.Summary, InputSchema: s.Service.Catalog.Schema(op), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}}, s.toolHandler(op.Tool))
	}
	sdk.AddTool(&mcp.Tool{Name: "tossinvest_get_server_status", Description: "Read server capabilities and readiness", InputSchema: contract.Object{"type": "object", "properties": contract.Object{}, "additionalProperties": false}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}}, s.toolHandler("tossinvest_get_server_status"))
	if s.Options.Milestone == "R2" {
		for name, schema := range core.StreamSchemas() {
			sdk.AddTool(&mcp.Tool{Name: name, Description: core.StreamDescription(name), InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: name != "tossinvest_set_subscriptions", IdempotentHint: true}}, s.toolHandler(name))
		}
	}
	mh := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return sdk }, &mcp.StreamableHTTPOptions{Stateless: true})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, contract.Object{"alive": true}) })
	mux.Handle("/mcp", s.auth(mh))
	mux.Handle("GET /admin/v1/status", s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, e := s.Service.Status(core.DefaultPrincipal())
		if e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, d)
	})))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.Options.Listen {
			http.Error(w, "host denied", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.Options.Listen {
			http.Error(w, "origin denied", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) auth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "Bearer " + s.credential
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			write(w, 401, contract.Object{"error": fault.New("AUTH_REQUIRED", "connection credential required")})
			return
		}
		h.ServeHTTP(w, r)
	})
}
func (s *Server) toolHandler(name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := contract.Object{}
		if len(req.Params.Arguments) > 0 {
			if contract.Decode(req.Params.Arguments, &args) != nil {
				return result(nil, fault.New("INVALID_ARGUMENT", "invalid JSON arguments")), nil
			}
		}
		data, e := s.Service.Call(ctx, core.DefaultPrincipal(), name, args)
		return result(data, e), nil
	}
}
func result(data any, e error) *mcp.CallToolResult {
	if e != nil {
		data = contract.Object{"error": fault.Wrap(e)}
	}
	b, _ := json.Marshal(data)
	return &mcp.CallToolResult{IsError: e != nil, StructuredContent: data, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, e error) {
	f := fault.Wrap(e)
	status := 500
	switch f.Code {
	case "INVALID_ARGUMENT", "CONFIG_CONFLICT", "CONFIG_INVALID":
		status = 400
	case "PERMISSION_DENIED", "AUTH_REQUIRED":
		status = 403
	case "NOT_READY":
		status = 503
	}
	write(w, status, contract.Object{"error": f})
}
func decodeRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		writeError(w, fault.New("INVALID_ARGUMENT", "invalid owner request"))
		return false
	}
	return true
}
func (s *Server) ownerHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /owner/v1/status", func(w http.ResponseWriter, r *http.Request) {
		rev, e := s.Service.Revision()
		if e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, contract.Object{"run_id": s.Service.RunID, "backend": s.Options.Backend, "listen": s.Options.Listen, "expected_access_revision": rev})
	})
	mux.HandleFunc("POST /owner/v1/accounts/discover", func(w http.ResponseWriter, r *http.Request) {
		d, e := s.Service.Discover(r.Context())
		if e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, d)
	})
	mux.HandleFunc("PUT /owner/v1/accounts/grant", func(w http.ResponseWriter, r *http.Request) {
		var g core.Grant
		if !decodeRequest(w, r, &g) {
			return
		}
		if e := s.Service.Grant(r.Context(), g); e != nil {
			writeError(w, e)
			return
		}
		write(w, 200, contract.Object{"applied": true})
	})
	return mux
}

type peerListener struct{ *net.UnixListener }

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		c, e := l.AcceptUnix()
		if e != nil {
			return nil, e
		}
		if platform.PeerAllowed(c) {
			return c, nil
		}
		c.Close()
	}
}
func (s *Server) URL() string { return "http://" + s.Options.Listen }
func (s *Server) ClientHTTP() *http.Client {
	return &http.Client{Transport: headerTransport{token: s.credential, next: http.DefaultTransport}, Timeout: 15 * time.Second}
}

type headerTransport struct {
	token string
	next  http.RoundTripper
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+h.token)
	return h.next.RoundTrip(r)
}
