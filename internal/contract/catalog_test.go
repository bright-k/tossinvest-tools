package contract_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	idx "github.com/bright-k/tossinvest-tools/docs/api"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	spec "github.com/bright-k/tossinvest-tools/specs/tossinvest"
	"github.com/shopspring/decimal"
	"strings"
	"testing"
)

func TestPinnedCoverageAndRefs(t *testing.T) {
	c := contract.Load()
	var manifest struct {
		Sources []struct {
			File  string
			SHA   string `json:"sha256"`
			Bytes int
		}
		Paths int `json:"rest_path_count"`
	}
	if json.Unmarshal(spec.Manifest, &manifest) != nil {
		t.Fatal("manifest")
	}
	for _, s := range manifest.Sources {
		b := spec.OpenAPI
		if s.File == "asyncapi.json" {
			b = spec.AsyncAPI
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != s.SHA || len(b) != s.Bytes {
			t.Fatal("snapshot hash/size changed")
		}
	}
	count := 0
	reads := 0
	seen := map[string]bool{}
	for _, op := range c.Ops {
		raw := c.Raw(op)
		if raw["operationId"] != op.ID {
			t.Fatal(op.ID)
		}
		count++
		if op.Exposure == "read_tool" {
			reads++
			if seen[op.Tool] {
				t.Fatal("duplicate tool")
			}
			seen[op.Tool] = true
		}
	}
	if count != 41 || reads != 34 || len(contract.Obj(c.Root["paths"])) != 38 {
		t.Fatal("REST coverage")
	}
	ops := contract.Obj(c.Async["operations"])
	if len(ops) != 10 || len(contract.Obj(c.Async["channels"])) != 4 {
		t.Fatal("WS coverage")
	}
	var index contract.Object
	_ = contract.Decode(idx.Operations, &index)
	for _, v := range contract.List(index["websocket_operations"]) {
		m := contract.Obj(v)
		id := contract.Str(m["operation_id"])
		if ops[id] == nil {
			t.Fatalf("missing WS %s", id)
		}
	}
	var check func(any, contract.Object)
	check = func(v any, root contract.Object) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
				var p any = root
				for _, k := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
					k = strings.ReplaceAll(strings.ReplaceAll(k, "~1", "/"), "~0", "~")
					p = contract.Obj(p)[k]
					if p == nil {
						t.Fatalf("broken reference %s", ref)
					}
				}
			}
			for _, p := range x {
				check(p, root)
			}
		case []any:
			for _, p := range x {
				check(p, root)
			}
		}
	}
	check(c.Root, c.Root)
	check(c.Async, c.Async)
}
func TestAllOfficialResponseExamples(t *testing.T) {
	c := contract.Load()
	n := 0
	for _, op := range c.Ops {
		if op.Method != "GET" {
			continue
		}
		for status := range contract.Obj(c.Raw(op)["responses"]) {
			for i, example := range c.Examples(op, status) {
				t.Run(fmt.Sprintf("%s/%s/%d", op.ID, status, i), func(t *testing.T) {
					n++
					if e := c.Validate(c.ResponseSchema(op, status), example, false); e != nil {
						t.Fatal(e)
					}
					b, _ := json.Marshal(example)
					code := 200
					if status != "200" {
						fmt.Sscan(status, &code)
					}
					if e := toss.DecodeTyped(op.ID, code, b); e != nil {
						t.Fatal(e)
					}
					var raw contract.Object
					if contract.Decode(b, &raw) != nil {
						t.Fatal("raw decode")
					}
					round, _ := json.Marshal(raw)
					var again contract.Object
					_ = contract.Decode(round, &again)
					if fmt.Sprint(raw) != fmt.Sprint(again) {
						t.Fatal("roundtrip changed")
					}
				})
			}
		}
	}
	t.Logf("decoded %d official read success/error examples", n)
}
func TestDecimalAndTime(t *testing.T) {
	a, _ := decimal.NewFromString("999999999999999999999999.000001")
	b, _ := decimal.NewFromString("0.000001")
	if !a.Mul(b).Equal(decimal.RequireFromString("999999999999999999.999999000001")) {
		t.Fatal("decimal multiplication")
	}
	c := contract.Load()
	s := contract.Object{"type": "string", "format": "date-time"}
	for _, x := range []string{"2026-11-01T01:30:00-04:00", "2026-11-01T01:30:00-05:00", "2026-03-25T09:00:00+09:00"} {
		if c.Validate(s, x, true) != nil {
			t.Fatal(x)
		}
	}
	if c.Validate(s, "2026-11-01T01:30:00", true) == nil {
		t.Fatal("offset missing")
	}
}
