package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	catalog "github.com/bright-k/tossinvest-tools/docs/api"
	spec "github.com/bright-k/tossinvest-tools/specs/tossinvest"
	"github.com/shopspring/decimal"
)

type Object = map[string]any
type Operation struct {
	RateGroup string `json:"-"`
	ID        string `json:"operation_id"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Tool      string `json:"mcp_tool"`
	Group     string `json:"group"`
	Summary   string `json:"summary"`
	Account   bool   `json:"requires_account_context"`
	Exposure  string `json:"exposure"`
}
type Catalog struct {
	Root  Object
	Async Object
	Ops   []Operation
}

func Decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}
func Load() *Catalog {
	c := &Catalog{}
	var index struct {
		Ops []Operation `json:"rest_operations"`
	}
	if err := Decode(spec.OpenAPI, &c.Root); err != nil {
		panic(err)
	}
	if err := Decode(spec.AsyncAPI, &c.Async); err != nil {
		panic(err)
	}
	if err := Decode(catalog.Operations, &index); err != nil {
		panic(err)
	}
	c.Ops = index.Ops
	for i := range c.Ops {
		description := Str(c.Raw(c.Ops[i])["description"])
		m := regexp.MustCompile("Rate Limits Group\\*\\*: `([^`]+)`").FindStringSubmatch(description)
		if len(m) == 2 {
			c.Ops[i].RateGroup = m[1]
		}
	}
	return c
}
func Obj(v any) Object { x, _ := v.(map[string]any); return x }
func List(v any) []any { x, _ := v.([]any); return x }
func Str(v any) string { x, _ := v.(string); return x }
func (c *Catalog) Resolve(s Object) Object {
	if ref := Str(s["$ref"]); ref != "" {
		var v any = c.Root
		for _, p := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			v = Obj(v)[p]
		}
		return Obj(v)
	}
	return s
}
func (c *Catalog) Raw(op Operation) Object {
	return Obj(Obj(Obj(c.Root["paths"])[op.Path])[strings.ToLower(op.Method)])
}
func (c *Catalog) Params(op Operation) []Object {
	var p []Object
	for _, v := range List(c.Raw(op)["parameters"]) {
		p = append(p, c.Resolve(Obj(v)))
	}
	return p
}
func (c *Catalog) Find(id string) (Operation, bool) {
	for _, o := range c.Ops {
		if o.ID == id || o.Tool == id {
			return o, true
		}
	}
	return Operation{}, false
}
func (c *Catalog) Schema(op Operation) Object {
	p := Object{}
	required := []any{}
	for _, v := range c.Params(op) {
		if v["in"] == "header" {
			continue
		}
		n := Str(v["name"])
		p[n] = c.Inline(Obj(v["schema"]))
		if v["required"] == true {
			required = append(required, n)
		}
	}
	if op.Account {
		p["account_alias"] = Object{"type": "string", "pattern": "^[a-z][a-z0-9_-]{0,31}$"}
		required = append(required, "account_alias")
	}
	return Object{"type": "object", "properties": p, "required": required, "additionalProperties": false}
}
func (c *Catalog) Inline(s Object) Object {
	s = c.Resolve(s)
	out := Object{}
	for k, v := range s {
		if k == "description" || k == "example" {
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			out[k] = c.Inline(x)
		case []any:
			a := []any{}
			for _, i := range x {
				if m, ok := i.(map[string]any); ok {
					a = append(a, c.Inline(m))
				} else {
					a = append(a, i)
				}
			}
			out[k] = a
		default:
			out[k] = v
		}
	}
	return out
}
func (c *Catalog) Validate(s Object, v any, request bool) error {
	s = c.Resolve(s)
	for _, x := range List(s["allOf"]) {
		if err := c.Validate(Obj(x), v, request); err != nil {
			return err
		}
	}
	if one := List(s["oneOf"]); len(one) > 0 {
		ok := false
		for _, x := range one {
			if c.Validate(Obj(x), v, request) == nil {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("no matching variant")
		}
	}
	if request {
		if en := List(s["enum"]); len(en) > 0 {
			ok := false
			for _, x := range en {
				if fmt.Sprint(x) == fmt.Sprint(v) {
					ok = true
				}
			}
			if !ok {
				return fmt.Errorf("invalid enum")
			}
		}
	}
	if v == nil {
		if s["type"] == nil || s["type"] == "null" {
			return nil
		}
		for _, t := range List(s["type"]) {
			if t == "null" {
				return nil
			}
		}
		return fmt.Errorf("unexpected null")
	}
	typ := Str(s["type"])
	if ts := List(s["type"]); len(ts) > 0 {
		for _, t := range ts {
			if t != "null" {
				typ = Str(t)
				break
			}
		}
	}
	switch typ {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object")
		}
		for _, r := range List(s["required"]) {
			if _, ok := m[Str(r)]; !ok {
				if !request && r == "englishName" && m["securityType"] == "ETF" {
					continue
				}
				return fmt.Errorf("missing %s", r)
			}
		}
		props := Obj(s["properties"])
		for k, x := range m {
			if sc, ok := props[k]; ok {
				if err := c.Validate(Obj(sc), x, request); err != nil {
					return fmt.Errorf("%s: %w", k, err)
				}
			} else if s["additionalProperties"] == false {
				return fmt.Errorf("unknown field %s", k)
			}
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			return fmt.Errorf("expected array")
		}
		if min := number(s["minItems"]); min > 0 && int64(len(a)) < min {
			return fmt.Errorf("too few items")
		}
		for _, x := range a {
			if err := c.Validate(Obj(s["items"]), x, request); err != nil {
				return err
			}
		}
	case "string":
		x, ok := v.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		if max := number(s["maxLength"]); max > 0 && int64(len(x)) > max {
			return fmt.Errorf("string too long")
		}
		if pat := Str(s["pattern"]); pat != "" && !regexp.MustCompile(pat).MatchString(x) {
			return fmt.Errorf("invalid pattern")
		}
		switch s["format"] {
		case "decimal":
			if _, err := decimal.NewFromString(x); err != nil {
				return fmt.Errorf("invalid decimal")
			}
		case "date":
			if _, err := time.Parse("2006-01-02", x); err != nil {
				return fmt.Errorf("invalid date")
			}
		case "date-time":
			if _, err := time.Parse(time.RFC3339Nano, x); err != nil {
				return fmt.Errorf("timestamp needs offset")
			}
		}
	case "integer", "number":
		n, ok := v.(json.Number)
		if !ok {
			return fmt.Errorf("expected number")
		}
		d, err := decimal.NewFromString(string(n))
		if err != nil {
			return err
		}
		if typ == "integer" && !d.Equal(d.Truncate(0)) {
			return fmt.Errorf("expected integer")
		}
		if min, ok := s["minimum"]; ok {
			a, _ := decimal.NewFromString(fmt.Sprint(min))
			if d.LessThan(a) {
				return fmt.Errorf("below minimum")
			}
		}
		if max, ok := s["maximum"]; ok {
			a, _ := decimal.NewFromString(fmt.Sprint(max))
			if d.GreaterThan(a) {
				return fmt.Errorf("above maximum")
			}
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	}
	return nil
}
func number(v any) int64 { n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64); return n }
func (c *Catalog) Input(op Operation, args Object) error {
	if err := c.Validate(c.Schema(op), args, true); err != nil {
		return err
	}
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) > 4096 {
			return fmt.Errorf("%s too long", k)
		}
	}
	if x, ok := args["symbols"].(string); ok {
		a := strings.Split(x, ",")
		if len(a) > 200 {
			return fmt.Errorf("maximum 200 symbols")
		}
		for _, s := range a {
			if s == "" {
				return fmt.Errorf("empty symbol")
			}
		}
	}
	if f, t := Str(args["from"]), Str(args["to"]); f != "" && t != "" && f > t {
		return fmt.Errorf("from after to")
	}
	if op.ID == "getRankings" && (args["type"] == "TOP_GAINERS" || args["type"] == "TOP_LOSERS") && args["duration"] == "realtime" {
		return fmt.Errorf("unsupported realtime ranking")
	}
	if op.ID == "getMarketIndicatorCandles" && strings.HasPrefix(Str(args["symbol"]), "KR_BOND_") && args["interval"] != "1d" {
		return fmt.Errorf("bond requires 1d")
	}
	return nil
}
func (c *Catalog) ResponseSchema(op Operation, status string) Object {
	r := c.Resolve(Obj(Obj(c.Raw(op)["responses"])[status]))
	return Obj(Obj(Obj(r["content"])["application/json"])["schema"])
}
func (c *Catalog) Examples(op Operation, status string) []Object {
	r := c.Resolve(Obj(Obj(c.Raw(op)["responses"])[status]))
	content := Obj(Obj(r["content"])["application/json"])
	out := []Object{}
	if v, ok := content["example"]; ok {
		out = append(out, Obj(v))
	}
	for _, x := range Obj(content["examples"]) {
		e := c.Resolve(Obj(x))
		if v, ok := e["value"]; ok {
			out = append(out, Obj(v))
		}
	}
	return out
}
