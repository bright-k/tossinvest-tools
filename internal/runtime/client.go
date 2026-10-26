package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/bright-k/tossinvest-tools/internal/platform"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func OwnerClient(root, backend string) (*http.Client, error) {
	dir := BackendDir(root, backend)
	if e := platform.PrivateDir(root); e != nil {
		return nil, fault.New("PERMISSION_DENIED", e.Error())
	}
	if e := platform.PrivateDir(dir); e != nil {
		return nil, fault.New("PERMISSION_DENIED", e.Error())
	}
	path := filepath.Join(dir, "owner.sock")
	st, e := os.Lstat(path)
	if e != nil {
		return nil, fault.New("SERVER_UNAVAILABLE", "start the server first")
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(sys.Uid) != os.Getuid() || st.Mode()&os.ModeSocket == 0 || st.Mode().Perm() != 0600 {
		return nil, fault.New("PERMISSION_DENIED", "invalid owner socket ownership or permissions")
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}}, nil
}
func OwnerRequest(ctx context.Context, client *http.Client, method, path string, input any) (contract.Object, error) {
	var body io.Reader
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return nil, e
		}
		body = bytes.NewReader(b)
	}
	r, e := http.NewRequestWithContext(ctx, method, "http://owner"+path, body)
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(r)
	if e != nil {
		return nil, fault.New("SERVER_UNAVAILABLE", "owner server unavailable")
	}
	defer resp.Body.Close()
	var v contract.Object
	if contract.Decode(readBound(resp.Body), &v) != nil {
		return nil, fault.New("UPSTREAM_DATA_INVALID", "invalid owner response")
	}
	if resp.StatusCode != 200 {
		b, _ := json.Marshal(v["error"])
		var f fault.Error
		if json.Unmarshal(b, &f) != nil {
			return nil, fault.New("INTERNAL_ERROR", "owner request failed")
		}
		return nil, &f
	}
	return v, nil
}
func readBound(r io.Reader) []byte { b, _ := io.ReadAll(io.LimitReader(r, 16<<20)); return b }
func ConnectionCredential(ctx context.Context, root, backend string) (Connection, error) {
	c := Connection{Backend: backend}
	if backend != "mock" && backend != "toss" {
		return c, fault.New("CONFIG_INVALID", "invalid backend")
	}
	if backend == "mock" {
		dir := BackendDir(root, backend)
		if e := platform.PrivateDir(root); e != nil {
			return c, fault.New("PERMISSION_DENIED", e.Error())
		}
		if e := platform.PrivateDir(dir); e != nil {
			return c, fault.New("PERMISSION_DENIED", e.Error())
		}
		file := filepath.Join(dir, "connection.json")
		if e := platform.CheckFile(file, 0600); e != nil {
			if os.IsNotExist(e) {
				return c, fault.New("SERVER_UNAVAILABLE", "mock server not running")
			}
			return c, fault.New("PERMISSION_DENIED", "mock credential file permission, ownership, or symlink denied")
		}
		b, e := os.ReadFile(file)
		if e != nil {
			return c, e
		}
		if json.Unmarshal(b, &c) != nil || c.Backend != "mock" || len(c.Credential) < 40 || c.RunID == "" {
			return c, fault.New("PERMISSION_DENIED", "invalid mock credential file")
		}
	} else {
		token, e := platform.ReadMCP()
		if e != nil {
			return c, e
		}
		c.Credential = token
	}
	owner, e := OwnerClient(root, backend)
	if e != nil {
		return c, e
	}
	state, e := OwnerRequest(ctx, owner, "GET", "/owner/v1/status", nil)
	if e != nil {
		return c, e
	}
	if state["backend"] != backend || backend == "mock" && state["run_id"] != c.RunID {
		return c, fault.New("PERMISSION_DENIED", "credential backend or run mismatch")
	}
	if backend == "toss" {
		c.RunID = contract.Str(state["run_id"])
		c.Listen = contract.Str(state["listen"])
	}
	if platform.ValidateListen(c.Listen) != nil || c.Listen != state["listen"] {
		return c, fault.New("PERMISSION_DENIED", "credential listen mismatch")
	}
	return c, nil
}
func Status(ctx context.Context, root, backend string) (contract.Object, error) {
	c, e := ConnectionCredential(ctx, root, backend)
	if e != nil {
		return nil, e
	}
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+c.Listen+"/admin/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+c.Credential)
	resp, e := client.Do(req)
	if e != nil {
		return nil, fault.New("SERVER_UNAVAILABLE", "HTTP server unavailable")
	}
	defer resp.Body.Close()
	var v contract.Object
	if contract.Decode(readBound(resp.Body), &v) != nil {
		return nil, fmt.Errorf("invalid status response")
	}
	if resp.StatusCode != 200 {
		return nil, fault.New("AUTH_REQUIRED", "status authentication rejected")
	}
	return v, nil
}
