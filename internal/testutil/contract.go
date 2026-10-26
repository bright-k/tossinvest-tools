// Package testutil supplies only public example data from the pinned specification.
package testutil

import (
	"encoding/json"
	"fmt"
	"github.com/bright-k/tossinvest-tools/internal/contract"
	"sort"
)

func Args(c *contract.Catalog, op contract.Operation, all bool) contract.Object {
	a := contract.Object{}
	for _, p := range c.Params(op) {
		if p["in"] == "header" {
			continue
		}
		if !all && p["required"] != true {
			continue
		}
		schema := c.Resolve(contract.Obj(p["schema"]))
		n := contract.Str(p["name"])
		var v any
		if x, ok := p["example"]; ok {
			v = x
		} else if examples := contract.Obj(p["examples"]); len(examples) > 0 {
			keys := []string{}
			for k := range examples {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			v = contract.Obj(examples[keys[0]])["value"]
		} else if x, ok := schema["default"]; ok {
			v = x
		} else if values := contract.List(schema["enum"]); len(values) > 0 {
			v = values[0]
		} else {
			switch schema["type"] {
			case "integer":
				v = json.Number("1")
			case "boolean":
				v = true
			default:
				v = "example"
			}
		}
		if n == "cursor" {
			v = "example_cursor"
		}
		if n == "from" {
			v = "2026-03-01"
		}
		if n == "to" {
			v = "2026-03-31"
		}
		if n == "symbol" && v == "example" {
			v = "005930"
		}
		if n == "orderId" {
			v = "app-new-order"
		}
		if n == "conditionalOrderId" {
			v = "app-new-conditional"
		}
		a[n] = v
	}
	if op.Account {
		a["account_alias"] = "primary"
	}
	if op.ID == "getMarketIndicatorCandles" {
		a["symbol"] = "KOSPI"
	}
	if op.ID == "getRankings" {
		a["type"] = "MARKET_TRADING_AMOUNT"
	}
	return a
}
func Body(c *contract.Catalog, op contract.Operation) any {
	examples := c.Examples(op, "200")
	if len(examples) == 0 {
		panic(fmt.Sprintf("no examples for %s", op.ID))
	}
	sort.Slice(examples, func(i, j int) bool {
		a, _ := json.Marshal(examples[i])
		b, _ := json.Marshal(examples[j])
		return len(a) > len(b)
	})
	return examples[0]["result"]
}
