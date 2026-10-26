package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/core"
	"github.com/bright-k/tossinvest-tools/internal/platform"
	run "github.com/bright-k/tossinvest-tools/internal/runtime"
)

func queryServer(t *testing.T) *run.Server {
	t.Helper()
	root, err := os.MkdirTemp("/private/tmp", "ti-cli-query-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	server, err := run.Start(context.Background(), run.Options{Root: root, Backend: "mock", Listen: address, Milestone: "R1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server
}

func callQueryCLI(t *testing.T, root string, arguments ...string) (int, contract.Object, string) {
	t.Helper()
	arguments = append(arguments, "--app-dir="+root, "--backend=mock")
	var out, stderr bytes.Buffer
	code := Main(arguments, &out, &stderr)
	var result contract.Object
	if code == 0 {
		if err := contract.Decode(out.Bytes(), &result); err != nil {
			t.Fatal("non-JSON CLI success")
		}
		if stderr.Len() != 0 {
			t.Fatal("unexpected diagnostic output on success")
		}
	} else {
		if out.Len() != 0 {
			t.Fatal("failed query printed success data")
		}
		if err := contract.Decode(stderr.Bytes(), &result); err != nil {
			t.Fatal("non-JSON CLI query error")
		}
	}
	return code, result, out.String() + stderr.String()
}

func allowQueryAccount(t *testing.T, server *run.Server) {
	t.Helper()
	client, err := run.OwnerClient(server.Options.Root, "mock")
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := run.OwnerRequest(context.Background(), client, "POST", "/owner/v1/accounts/discover", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := contract.Str(discovery["discovery_id"])
	revision, _ := discovery["expected_access_revision"].(json.Number).Int64()
	first := contract.Obj(contract.List(discovery["accounts"])[0])
	_, err = run.OwnerRequest(context.Background(), client, "PUT", "/owner/v1/accounts/grant", core.Grant{
		DiscoveryID: &id, Revision: revision, Principal: core.PrincipalID,
		Selected: []core.Selection{{Handle: contract.Str(first["handle"]), Alias: "primary"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLIQueriesUseAuthenticatedServerAndAccountGrants(t *testing.T) {
	server := queryServer(t)
	root := server.Options.Root
	code, result, _ := callQueryCLI(t, root, "accounts", "--json")
	if code != 0 || len(contract.List(result["data"])) != 0 || contract.Obj(result["meta"])["account_selection_required"] != true {
		t.Fatal("initial accounts must be the empty local allowed set")
	}
	code, result, _ = callQueryCLI(t, root, "prices", "--symbols=005930,AAPL")
	if code != 0 || len(contract.List(result["data"])) == 0 {
		t.Fatal("market query unavailable before account selection")
	}
	for _, alias := range []string{"primary", "unselected"} {
		code, result, _ = callQueryCLI(t, root, "holdings", "--account-alias="+alias)
		if code != 4 || contract.Obj(result["error"])["code"] != "PERMISSION_DENIED" {
			t.Fatal("unselected account allowed")
		}
	}
	allowQueryAccount(t, server)
	code, result, text := callQueryCLI(t, root, "accounts")
	accounts := contract.List(result["data"])
	if code != 0 || len(accounts) != 1 || contract.Obj(accounts[0])["account_alias"] != "primary" || contract.Obj(accounts[0])["account_no_masked"] != "****1111" {
		t.Fatal("accounts did not use masked server grant list")
	}
	if strings.Contains(text, "2222") {
		t.Fatal("unselected account disclosed")
	}
	code, result, text = callQueryCLI(t, root, "holdings", "--account-alias=primary", "--symbol=005930", "--json")
	if code != 0 || contract.Obj(result["data"])["items"] == nil || contract.Obj(result["meta"])["account_alias"] != "primary" {
		t.Fatal("holdings query failed")
	}
	for _, forbidden := range []string{"accountSeq", "accountNo", "mock-only-secret", "mock-only-access"} {
		if strings.Contains(text, forbidden) {
			t.Fatal("sensitive field leaked to CLI")
		}
	}
	file, err := os.ReadFile(filepath.Join(root, "mock", "connection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var connection run.Connection
	if json.Unmarshal(file, &connection) != nil {
		t.Fatal("mock connection decode")
	}
	if strings.Contains(text, connection.Credential) {
		t.Fatal("MCP credential leaked")
	}
	revision, err := server.Service.Revision()
	if err != nil {
		t.Fatal(err)
	}
	if err = server.Service.Grant(context.Background(), core.Grant{Revision: revision, Principal: core.PrincipalID, Selected: []core.Selection{}}); err != nil {
		t.Fatal(err)
	}
	code, result, _ = callQueryCLI(t, root, "holdings", "--account-alias=primary")
	if code != 4 || contract.Obj(result["error"])["code"] != "PERMISSION_DENIED" {
		t.Fatal("revoked account still readable from CLI")
	}
}

func TestCLIQueriesValidateBeforeConnectionAndDoNotFallback(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	for _, arguments := range [][]string{
		{"holdings"}, {"holdings", "--account-alias=12345"}, {"holdings", "--account-alias=primary", "--symbol="}, {"prices"}, {"prices", "--symbols=AAPL,,MSFT"},
		{"prices", "--symbols=" + strings.Repeat("A,", 200) + "A"},
	} {
		code, result, _ := callQueryCLI(t, root, arguments...)
		if code != 2 || contract.Obj(result["error"])["code"] != "INVALID_ARGUMENT" {
			t.Fatal("invalid input attempted connection")
		}
	}
	for _, arguments := range [][]string{{"accounts"}, {"prices", "--symbols=AAPL"}, {"holdings", "--account-alias=primary"}} {
		code, result, _ := callQueryCLI(t, root, arguments...)
		if code != 3 || contract.Obj(result["error"])["code"] != "SERVER_UNAVAILABLE" {
			t.Fatal("query used a fallback without a server")
		}
	}
	for _, arguments := range [][]string{{"accounts", "--accountSeq=1"}, {"holdings", "--account=1"}, {"prices", "--token=secret"}, {"accounts", "--symbols=AAPL"}} {
		var out, stderr bytes.Buffer
		arguments = append(arguments, "--app-dir="+root, "--backend=mock")
		if Main(arguments, &out, &stderr) != 2 {
			t.Fatal("unsupported or credential flag accepted")
		}
	}
}

func TestCLIQueryRejectsStaleCredentialAndUsesConfiguredBackend(t *testing.T) {
	server := queryServer(t)
	root := server.Options.Root
	if err := platform.AtomicJSON(filepath.Join(root, "config.json"), run.Config{Schema: 1, Backend: "mock", Listen: server.Options.Listen}); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if Main([]string{"prices", "--symbols=AAPL", "--app-dir=" + root}, &out, &stderr) != 0 {
		t.Fatal("configured mock backend not honored")
	}
	file := filepath.Join(root, "mock", "connection.json")
	encoded, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var connection run.Connection
	if json.Unmarshal(encoded, &connection) != nil {
		t.Fatal("mock connection decode")
	}
	connection.Credential = core.Random()
	if err = platform.AtomicJSON(file, connection); err != nil {
		t.Fatal(err)
	}
	code, result, _ := callQueryCLI(t, root, "prices", "--symbols=AAPL")
	if code != 4 || contract.Obj(result["error"])["code"] != "AUTH_REQUIRED" {
		t.Fatal("wrong MCP credential accepted or misclassified")
	}
}

func TestClearAccountsNeverCallsUpstreamDiscovery(t *testing.T) {
	server := queryServer(t)
	allowQueryAccount(t, server)
	broker := server.Service.Broker.(*toss.REST)
	next := broker.HTTP.Transport
	var discoveries atomic.Int32
	broker.HTTP.Transport = blockingDiscoveryTransport{next: next, discoveries: &discoveries}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = old; reader.Close() }()
	_, _ = writer.WriteString("YES\n")
	writer.Close()
	var out, stderr bytes.Buffer
	if code := setupAccounts(context.Background(), server.Options.Root, "mock", core.PrincipalID, true, &out, &stderr); code != 0 {
		t.Fatal("clear failed without upstream")
	}
	if discoveries.Load() != 0 {
		t.Fatal("clear contacted upstream account discovery")
	}
}

type blockingDiscoveryTransport struct {
	next        http.RoundTripper
	discoveries *atomic.Int32
}

func (t blockingDiscoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/api/v1/accounts" {
		t.discoveries.Add(1)
		return nil, fault.New("UPSTREAM_ERROR", "simulated offline discovery")
	}
	return t.next.RoundTrip(r)
}
