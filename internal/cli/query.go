package cli

import (
	"flag"

	"github.com/bright-k/tossinvest-tools/internal/contract"
)

type queryFlags struct {
	accountAlias string
	symbol       string
	symbolSet    bool
	symbols      string
}

func (q *queryFlags) register(flags *flag.FlagSet, command string) {
	switch command {
	case "holdings":
		flags.StringVar(&q.accountAlias, "account-alias", "", "required allowed account alias")
		flags.Func("symbol", "optional holdings symbol filter", func(value string) error { q.symbol = value; q.symbolSet = true; return nil })
	case "prices":
		flags.StringVar(&q.symbols, "symbols", "", "required comma-separated symbols, maximum 200")
	}
}

func (q queryFlags) request(command string) (string, contract.Object) {
	switch command {
	case "accounts":
		return "tossinvest_get_accounts", contract.Object{}
	case "holdings":
		args := contract.Object{"account_alias": q.accountAlias}
		if q.symbolSet {
			args["symbol"] = q.symbol
		}
		return "tossinvest_get_holdings", args
	case "prices":
		return "tossinvest_get_prices", contract.Object{"symbols": q.symbols}
	}
	panic("unregistered CLI read command")
}
