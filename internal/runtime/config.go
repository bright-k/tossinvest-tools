package runtime

import (
	"bytes"
	"encoding/json"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"github.com/bright-k/tossinvest-tools/internal/platform"
	"io"
	"os"
	"path/filepath"
)

type Config struct {
	Schema        int    `json:"schema_version"`
	Backend       string `json:"backend"`
	Listen        string `json:"listen"`
	TradingPolicy any    `json:"trading_policy"`
}

func DefaultRoot() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Library", "Application Support", "tossinvest-tools")
}
func Load(root string) (Config, error) {
	c := Config{Schema: 1, Backend: "toss", Listen: "127.0.0.1:17840"}
	b, e := os.ReadFile(filepath.Join(root, "config.json"))
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return c, fault.New("CONFIG_INVALID", "invalid config schema")
	}
	if d.Decode(new(any)) != io.EOF {
		return c, fault.New("CONFIG_INVALID", "trailing config JSON")
	}
	if c.TradingPolicy != nil {
		return c, fault.New("CONFIG_INVALID", "trading policy is unavailable in readonly R2")
	}
	if c.Schema != 1 || c.Backend != "toss" && c.Backend != "mock" || platform.ValidateListen(c.Listen) != nil {
		return c, fault.New("CONFIG_INVALID", "invalid config values")
	}
	return c, nil
}
func BackendDir(root, backend string) string {
	if backend == "mock" {
		return filepath.Join(root, "mock")
	}
	return root
}
