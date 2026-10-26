package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/core"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/bright-k/tossinvest-tools/internal/platform"
	run "github.com/bright-k/tossinvest-tools/internal/runtime"
	"github.com/bright-k/tossinvest-tools/internal/store"
	"golang.org/x/term"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const Help = `tossinvest-tools (readonly R2)
  setup [accounts]  Local hidden credential input / allowed account selection (TTY)
  serve            Independent HTTP MCP server; --mode=trading is unavailable
  accounts         List allowed, masked accounts through the running server
  holdings --account-alias=ALIAS [--symbol=SYMBOL]  Read allowed account holdings
  prices --symbols=SYMBOL[,SYMBOL...]              Read current prices
  doctor | status  Authenticated readiness diagnosis (--json)
  mcp-auth-header  Machine-only MCP connection header helper
  service install|status|stop|uninstall [--dry-run]
  backup --output=PATH | restore --input=PATH  Offline SQLite backup/recovery
  version
Options: --backend=toss|mock --app-dir=PATH --listen=127.0.0.1:17840
serve: --milestone=R1|R2 (default R2); setup accounts: --clear --principal=codex-readonly
Read commands return the server data/meta JSON (--json is also accepted).
No order, credential CLI arguments, or remote upstream URL options exist.
`

func Main(args []string, out, errout io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(out, Help)
		return 0
	}
	command := args[0]
	if command == "version" {
		emit(out, contract.Object{"version": run.Version, "rest_api": "1.2.24", "websocket_api": "1.2.2", "go": "1.27.3", "mode": "readonly"})
		return 0
	}
	sub := ""
	args = args[1:]
	if (command == "setup" || command == "service") && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		args = args[1:]
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(errout)
	root := f.String("app-dir", run.DefaultRoot(), "app root")
	backend := f.String("backend", "", "toss or mock")
	listen := f.String("listen", "", "loopback listen")
	mode := f.String("mode", "readonly", "readonly only")
	milestone := f.String("milestone", "R2", "R1 or R2")
	jsonOut := f.Bool("json", false, "JSON output")
	clear := f.Bool("clear", false, "revoke all allowed accounts")
	principal := f.String("principal", core.PrincipalID, "connection principal")
	dry := f.Bool("dry-run", false, "print service configuration only")
	output := f.String("output", "", "backup destination")
	input := f.String("input", "", "restore source")
	var query queryFlags
	query.register(f, command)
	if f.Parse(args) != nil || f.NArg() != 0 {
		return 2
	}
	_ = jsonOut
	cfg, e := run.Load(*root)
	if e != nil {
		return fail(errout, e)
	}
	if *backend == "" {
		*backend = cfg.Backend
	}
	if *listen == "" {
		*listen = cfg.Listen
	}
	if *backend != "toss" && *backend != "mock" {
		return fail(errout, fault.New("CONFIG_INVALID", "backend must be toss or mock"))
	}
	if *mode != "readonly" {
		return fail(errout, fault.New("CAPABILITY_UNAVAILABLE", "trading module is not implemented; readonly R2 only"))
	}
	ctx := context.Background()
	switch command {
	case "accounts", "holdings", "prices":
		tool, args := query.request(command)
		result, err := run.Query(ctx, *root, *backend, tool, args)
		if err != nil {
			return fail(errout, err)
		}
		emit(out, result)
		return 0
	case "serve":
		ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer cancel()
		s, e := run.Start(ctx, run.Options{Root: *root, Backend: *backend, Listen: *listen, Mode: *mode, Milestone: *milestone})
		if e != nil {
			return fail(errout, e)
		}
		defer s.Close()
		logPath := filepath.Join(*root, "logs")
		if *root == run.DefaultRoot() {
			h, _ := os.UserHomeDir()
			logPath = filepath.Join(h, "Library", "Logs", "tossinvest-tools")
		}
		_ = logLine(logPath, contract.Object{"event": "server_started", "mode": "readonly", "backend": *backend, "listen": *listen})
		emit(out, contract.Object{"server": "TossInvest Tools Server", "mode": "readonly", "backend": *backend, "mcp_url": s.URL() + "/mcp", "milestone": *milestone})
		select {
		case <-ctx.Done():
			_ = logLine(logPath, contract.Object{"event": "server_stopped"})
			return 0
		case e := <-s.Errors:
			return fail(errout, e)
		}
	case "setup":
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fail(errout, fault.New("CONFIG_INVALID", "setup requires a local interactive TTY"))
		}
		if sub == "accounts" {
			return setupAccounts(ctx, *root, *backend, *principal, *clear, out, errout)
		}
		if sub != "" {
			return 2
		}
		if e := platform.PrivateDir(*root); e != nil {
			return fail(errout, e)
		}
		if *backend == "toss" {
			fmt.Fprint(errout, "Toss client ID (hidden): ")
			id, e := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(errout)
			if e != nil {
				return fail(errout, e)
			}
			fmt.Fprint(errout, "Toss client secret (hidden): ")
			secret, e := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(errout)
			if e != nil {
				return fail(errout, e)
			}
			if len(id) == 0 || len(secret) == 0 {
				return fail(errout, fault.New("CONFIG_INVALID", "credentials must not be empty"))
			}
			if e = platform.SaveToss(toss.Credentials{ClientID: string(id), ClientSecret: string(secret)}); e != nil {
				return fail(errout, e)
			}
			if e = platform.SaveMCP(core.Random()); e != nil {
				return fail(errout, e)
			}
		}
		if e := platform.AtomicJSON(filepath.Join(*root, "config.json"), run.Config{Schema: 1, Backend: *backend, Listen: *listen}); e != nil {
			return fail(errout, e)
		}
		emit(out, contract.Object{"configured": true, "backend": *backend, "next": "start serve, then setup accounts; restart existing MCP connection after credential changes"})
		return 0
	case "status", "doctor":
		d, e := run.Status(ctx, *root, *backend)
		if e != nil {
			return fail(errout, e)
		}
		emit(out, d)
		if command == "doctor" && diagnosisIncomplete(d) {
			return 5
		}
		return 0
	case "mcp-auth-header":
		c, e := run.ConnectionCredential(ctx, *root, *backend)
		if e != nil {
			return fail(errout, e)
		}
		emit(out, map[string]string{"Authorization": "Bearer " + c.Credential})
		return 0
	case "service":
		executable, e := os.Executable()
		if e != nil {
			return fail(errout, e)
		}
		plist := platform.ServicePath()
		if *dry {
			if sub != "install" {
				return 2
			}
			fmt.Fprint(out, platform.LaunchPlist(executable, *root, *backend, *listen))
			return 0
		}
		switch sub {
		case "install":
			if _, e := os.Stat(plist); e == nil {
				return fail(errout, fault.New("CONFIG_CONFLICT", "service exists; uninstall explicitly before replacing"))
			}
			if e = os.MkdirAll(filepath.Dir(plist), 0700); e == nil {
				e = os.WriteFile(plist, []byte(platform.LaunchPlist(executable, *root, *backend, *listen)), 0600)
			}
			if e == nil {
				e = platform.ServiceAction("install", plist)
			}
		case "status":
			state := contract.Object{"registered": false, "registered_mode": nil, "registered_backend": nil}
			if b, err := os.ReadFile(plist); err == nil {
				state["registered"] = true
				state["registered_mode"] = "unknown"
				if strings.Contains(string(b), "--mode=readonly") {
					state["registered_mode"] = "readonly"
				}
				for _, name := range []string{"mock", "toss"} {
					if strings.Contains(string(b), "--backend="+name) {
						state["registered_backend"] = name
					}
				}
			}
			state["launchd_loaded"] = platform.ServiceAction("status", plist) == nil
			if d, err := run.Status(ctx, *root, *backend); err == nil {
				state["running"] = d
			} else {
				state["running"] = false
			}
			emit(out, state)
			return 0
		case "stop":
			e = platform.ServiceAction("stop", plist)
		case "uninstall":
			_ = platform.ServiceAction("uninstall", plist)
			e = os.Remove(plist)
			if os.IsNotExist(e) {
				e = nil
			}
		default:
			return 2
		}
		if e != nil {
			return fail(errout, e)
		}
		emit(out, contract.Object{"action": sub, "mode": "readonly", "completed": true})
		return 0
	case "backup", "restore":
		if e := platform.PrivateDir(*root); e != nil {
			return fail(errout, e)
		}
		lock, e := platform.Acquire(filepath.Join(*root, "server.lock"))
		if e != nil {
			return fail(errout, fault.New("CONFIG_CONFLICT", "stop server before offline backup or restore"))
		}
		defer lock.Close()
		dir := run.BackendDir(*root, *backend)
		path := filepath.Join(dir, "state.sqlite3")
		if command == "backup" {
			if *output == "" {
				return 2
			}
			db, e := store.Open(path, *backend)
			if e != nil {
				return fail(errout, e)
			}
			defer db.Close()
			if e = db.Check(); e == nil {
				e = db.Backup(*output)
			}
			if e != nil {
				return fail(errout, e)
			}
			emit(out, contract.Object{"backup_completed": true})
			return 0
		}
		if *input == "" {
			return 2
		}
		if e = platform.CheckFile(*input, 0600); e != nil {
			return fail(errout, e)
		}
		if _, e = os.Stat(path); e == nil {
			return fail(errout, fault.New("CONFIG_CONFLICT", "restore requires absent target DB; preserve existing DB and WAL offline first"))
		}
		if e = platform.PrivateDir(dir); e != nil {
			return fail(errout, e)
		}
		b, e := os.ReadFile(*input)
		if e != nil {
			return fail(errout, e)
		}
		if e = platform.Atomic(path, b); e != nil {
			return fail(errout, e)
		}
		db, e := store.Open(path, *backend)
		if e != nil {
			_ = os.Rename(path, path+".rejected")
			return fail(errout, e)
		}
		e = db.Check()
		db.Close()
		if e != nil {
			return fail(errout, e)
		}
		emit(out, contract.Object{"restored": true, "cursors_expire_on_restart": true, "resynchronization_required": true})
		return 0
	default:
		fmt.Fprint(errout, Help)
		return 2
	}
}
func setupAccounts(ctx context.Context, root, backend, principal string, clear bool, out, errout io.Writer) int {
	client, e := run.OwnerClient(root, backend)
	if e != nil {
		return fail(errout, e)
	}
	// Discovery carries only masked accounts and short-lived handles.
	var d contract.Object
	if !clear {
		d, e = run.OwnerRequest(ctx, client, "POST", "/owner/v1/accounts/discover", nil)
	}
	if clear {
		// Revocation must not depend on upstream availability. Read the local revision through a dedicated masked discovery-free owner status field.
		d, e = run.OwnerRequest(ctx, client, "GET", "/owner/v1/status", nil)
	}
	if e != nil {
		return fail(errout, e)
	}
	g := core.Grant{Principal: principal, Selected: []core.Selection{}}
	rev, _ := d["expected_access_revision"].(json.Number)
	g.Revision, _ = rev.Int64()
	reader := bufio.NewReader(os.Stdin)
	if !clear {
		items := contract.List(d["accounts"])
		for i, v := range items {
			a := contract.Obj(v)
			fmt.Fprintf(out, "%d. %s %s (suggested %s)\n", i+1, a["account_type"], a["account_no_masked"], a["suggested_alias"])
		}
		fmt.Fprint(out, "Select index:alias pairs separated by comma (empty revokes all): ")
		line, e := reader.ReadString('\n')
		if e != nil {
			return fail(errout, e)
		}
		line = strings.TrimSpace(line)
		if line != "" {
			for _, pair := range strings.Split(line, ",") {
				parts := strings.SplitN(strings.TrimSpace(pair), ":", 2)
				if len(parts) != 2 {
					return 2
				}
				n, e := strconv.Atoi(parts[0])
				if e != nil || n < 1 || n > len(items) {
					return 2
				}
				g.Selected = append(g.Selected, core.Selection{Handle: contract.Str(contract.Obj(items[n-1])["handle"]), Alias: parts[1]})
			}
		}
		id := contract.Str(d["discovery_id"])
		g.DiscoveryID = &id
	}
	fmt.Fprintf(out, "Replace grants for %s with %d account(s). Type YES: ", principal, len(g.Selected))
	line, e := reader.ReadString('\n')
	if e != nil {
		return fail(errout, e)
	}
	if strings.TrimSpace(line) != "YES" {
		return fail(errout, fault.New("CONFIG_INVALID", "selection not confirmed"))
	}
	_, e = run.OwnerRequest(ctx, client, "PUT", "/owner/v1/accounts/grant", g)
	if e != nil {
		return fail(errout, e)
	}
	emit(out, contract.Object{"applied": true, "selected_count": len(g.Selected)})
	return 0
}
func emit(w io.Writer, v any) { _ = json.NewEncoder(w).Encode(v) }
func fail(w io.Writer, e error) int {
	f := fault.Wrap(e)
	emit(w, contract.Object{"error": f})
	switch f.Code {
	case "INVALID_ARGUMENT", "CONFIG_INVALID", "CONFIG_CONFLICT", "CAPABILITY_UNAVAILABLE":
		return 2
	case "SERVER_UNAVAILABLE":
		return 3
	case "PERMISSION_DENIED", "AUTH_REQUIRED":
		return 4
	case "NOT_READY":
		return 5
	case "OUTCOME_UNKNOWN":
		return 6
	}
	return 1
}
func logLine(dir string, v contract.Object) error {
	if e := platform.PrivateDir(dir); e != nil {
		return e
	}
	path := filepath.Join(dir, "server.jsonl")
	if st, e := os.Stat(path); e == nil && st.Size() > 1<<20 {
		_ = os.Remove(path + ".3")
		for i := 2; i >= 1; i-- {
			_ = os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
		}
		_ = os.Rename(path, path+".1")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	v["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	return json.NewEncoder(f).Encode(v)
}

func diagnosisIncomplete(d contract.Object) bool {
	return d["query_ready"] != true || (d["milestone"] == "R2" && d["streams_ready"] != true)
}
