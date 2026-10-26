package cli

import (
	"bytes"
	"github.com/bright-k/tossinvest-tools/internal/platform"
	"github.com/bright-k/tossinvest-tools/internal/runtime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIExitCodesNoTTYAndTradingDenied(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	for _, tc := range []struct {
		args []string
		want int
	}{{[]string{"serve", "--backend=mock", "--mode=trading"}, 2}, {[]string{"serve", "--backend=toss", "--mode=trading"}, 2}, {[]string{"setup", "accounts", "--backend=mock"}, 2}, {[]string{"status", "--backend=mock"}, 3}, {[]string{"doctor", "--backend=mock"}, 3}, {[]string{"order", "--approved=true"}, 2}} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, err bytes.Buffer
			args := append(tc.args, "--app-dir="+dir)
			got := Main(args, &out, &err)
			if got != tc.want {
				t.Fatalf("got %d want %d: %s", got, tc.want, err.String())
			}
		})
	}
}
func TestConfigRejectsModeScopeGrantAndUnknownFields(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{`{"schema_version":1,"mode":"trading"}`, `{"schema_version":1,"scope":["owner"]}`, `{"schema_version":1,"account_aliases":{"primary":1}}`, `{"schema_version":2}`, `{"schema_version":1} {}`, `{"schema_version":1,"trading_policy":{"enabled":true}}`, `{"schema_version":1,"listen":"0.0.0.0:17840"}`} {
		os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0600)
		if _, e := runtime.Load(dir); e == nil {
			t.Fatal("unsafe config accepted")
		}
	}
}
func TestServiceDryRunReadonlyAndNoInstallation(t *testing.T) {
	dir := t.TempDir()
	var out, err bytes.Buffer
	if got := Main([]string{"service", "install", "--dry-run", "--backend=mock", "--app-dir=" + dir}, &out, &err); got != 0 {
		t.Fatal(got, err.String())
	}
	if !strings.Contains(out.String(), "--mode=readonly") || strings.Contains(out.String(), "trading") {
		t.Fatal("plist profile")
	}
	plist := platform.LaunchPlist("/tmp/path & space/tool", "/tmp/root", "mock", "127.0.0.1:17840")
	if !strings.Contains(plist, "&amp;") || !strings.Contains(plist, "ThrottleInterval") {
		t.Fatal("plist escaping or restart limit")
	}
	if _, e := os.Stat(filepath.Join(dir, "owner.sock")); !os.IsNotExist(e) {
		t.Fatal("dry run started server")
	}
}

func TestDoctorFeatureReadiness(t *testing.T) {
	for _, tc := range []struct {
		state      map[string]any
		incomplete bool
	}{{map[string]any{"milestone": "R1", "query_ready": true, "streams_ready": false}, false}, {map[string]any{"milestone": "R2", "query_ready": true, "streams_ready": false}, true}, {map[string]any{"milestone": "R2", "query_ready": true, "streams_ready": true}, false}, {map[string]any{"milestone": "R1", "query_ready": false}, true}} {
		if diagnosisIncomplete(tc.state) != tc.incomplete {
			t.Fatal("partial diagnostic readiness misreported")
		}
	}
}
