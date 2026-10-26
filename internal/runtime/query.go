package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Query invokes one of the three CLI read tools through the running MCP server.
// Only its connection credential is read here; Toss credentials, account grants,
// token refresh, and all broker requests remain owned by the server.
func Query(ctx context.Context, root, backend, tool string, args contract.Object) (contract.Object, error) {
	switch tool {
	case "tossinvest_get_accounts", "tossinvest_get_holdings", "tossinvest_get_prices":
	default:
		return nil, fault.New("READONLY_VIOLATION", "unsupported CLI read tool")
	}
	catalog := contract.Load()
	op, _ := catalog.Find(tool)
	if err := catalog.Input(op, args); err != nil {
		return nil, fault.New("INVALID_ARGUMENT", err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	connection, err := ConnectionCredential(ctx, root, backend)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // A loopback connection must never go through a proxy.
	defer transport.CloseIdleConnections()
	origin := "http://" + connection.Listen
	httpClient := &http.Client{
		Timeout:   20 * time.Second,
		Transport: &queryTransport{origin: origin, token: connection.Credential, next: transport},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fault.New("PERMISSION_DENIED", "local MCP redirect denied")
		},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "tossinvest-tools-cli", Version: Version}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: origin + "/mcp", HTTPClient: httpClient}, nil)
	if err != nil {
		return nil, queryConnectionError(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, queryConnectionError(err)
	}
	return decodeQueryResult(result, backend)
}

type queryTransport struct {
	origin, token string
	next          http.RoundTripper
}

func (t *queryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme+"://"+request.URL.Host != t.origin || request.URL.User != nil {
		return nil, fault.New("PERMISSION_DENIED", "local MCP origin denied")
	}
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+t.token)
	response, err := t.next.RoundTrip(request)
	if err != nil {
		return nil, fault.New("SERVER_UNAVAILABLE", "local MCP server unavailable")
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		response.Body.Close()
		code := "AUTH_REQUIRED"
		if response.StatusCode == http.StatusForbidden {
			code = "PERMISSION_DENIED"
		}
		return nil, fault.New(code, "local MCP authentication or access denied")
	}
	return response, nil
}

func queryConnectionError(err error) error {
	var local *fault.Error
	if errors.As(err, &local) {
		return local
	}
	// SDK/network errors can contain URLs or protocol details; never echo headers.
	return fault.New("SERVER_UNAVAILABLE", "local MCP connection or request failed")
}

func decodeQueryResult(result *mcp.CallToolResult, backend string) (contract.Object, error) {
	invalid := func() (contract.Object, error) {
		return nil, fault.New("UPSTREAM_DATA_INVALID", "invalid local MCP query response")
	}
	if result == nil || result.StructuredContent == nil || len(result.Content) != 1 {
		return invalid()
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return invalid()
	}
	// Our server emits the same canonical JSON as structured content and text.
	// Decode the text copy with UseNumber so SDK decoding through any cannot
	// round large integers or change Decimal strings, nulls, or future fields.
	decoder := json.NewDecoder(bytes.NewBufferString(text.Text))
	decoder.UseNumber()
	var body contract.Object
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		return invalid()
	}
	if result.IsError {
		encoded, err := json.Marshal(body["error"])
		if err != nil {
			return invalid()
		}
		var serverError fault.Error
		if contract.Decode(encoded, &serverError) != nil || serverError.Code == "" {
			return invalid()
		}
		return nil, &serverError
	}
	if _, ok := body["data"]; !ok || body["error"] != nil {
		return invalid()
	}
	meta := contract.Obj(body["meta"])
	if meta["backend"] != backend || meta["mode"] != "readonly" || meta["schema_version"] != json.Number("1") {
		return invalid()
	}
	return body, nil
}
