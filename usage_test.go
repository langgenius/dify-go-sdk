package dify

import (
	"errors"
	"testing"
)

func TestAnAbsentPriceIsUnknownNotFree(t *testing.T) {
	u := usageFrom(object{"total_tokens": 5}, nil)
	if u.CostKnown() {
		t.Error("nobody said what it cost")
	}
	if _, _, err := u.TotalPrice(); !errors.Is(err, ErrCostUnknown) {
		t.Errorf("got %v", err)
	}
	if u.String() != "5 tokens (0 in / 0 out) · cost not reported" {
		t.Errorf("got %q", u.String())
	}
}

func TestAReportedZeroIsFree(t *testing.T) {
	u := usageFrom(object{"total_tokens": 5, "total_price": "0"}, nil)
	price, _, err := u.TotalPrice()
	if !u.CostKnown() || err != nil || !price.IsZero() {
		t.Errorf("a reported zero is a reported figure: %v %v", u, err)
	}
}

func TestUnknownPlusANumberIsThatNumber(t *testing.T) {
	a := Usage{TotalTokens: 1}
	b := Usage{TotalTokens: 2, Costs: map[string]Amount{"USD": mustAmount(t, "0.001")}}
	sum := a.Add(b)
	price, _, err := sum.TotalPrice()
	if err != nil || price.String() != "0.001" || sum.TotalTokens != 3 {
		t.Errorf("got %v %v", sum, err)
	}
	if a.Add(Usage{}).CostKnown() {
		t.Error("unknown plus unknown stays unknown")
	}
}

func TestAmountsAddExactly(t *testing.T) {
	var total Amount
	for range 10 {
		total = total.Add(mustAmount(t, "0.1"))
	}
	if total.String() != "1.0" {
		t.Errorf("ten dimes are a dollar, got %s", total)
	}
}

func TestSeveralCurrenciesAreNotAddedTogether(t *testing.T) {
	u := Usage{Costs: map[string]Amount{"USD": mustAmount(t, "1"), "RMB": mustAmount(t, "2")}}
	if _, _, err := u.TotalPrice(); err == nil {
		t.Error("there is no exchange rate here")
	}
}

func TestAPricedNodeOutranksAZeroRunTotal(t *testing.T) {
	// A run total of "0" is what Dify sends for a model it has no pricing for.
	nodes := Usage{TotalTokens: 10, Costs: map[string]Amount{"USD": mustAmount(t, "0.02")}}
	total := usageFrom(object{"total_tokens": 12, "total_price": "0"}, nil)
	merged := nodes.mergedWith(total)
	price, _, _ := merged.TotalPrice()
	if merged.TotalTokens != 12 || price.String() != "0.02" {
		t.Errorf("a billed run must not read as free: %v", merged)
	}
}

func mustAmount(t *testing.T, s string) Amount {
	t.Helper()
	a, err := ParseAmount(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
