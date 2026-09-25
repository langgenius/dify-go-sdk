package dify

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
)

// Amount is a decimal money figure, kept exact. Dify reports prices as
// decimal strings ("0.000214"); a float64 would add rounding error on every
// sum, and a budget check is exactly where that is not acceptable.
type Amount struct {
	r     big.Rat
	scale int
}

// ParseAmount reads a decimal string such as "0.000214".
func ParseAmount(s string) (Amount, error) {
	var a Amount
	s = strings.TrimSpace(s)
	if _, ok := a.r.SetString(s); !ok {
		return Amount{}, fmt.Errorf("dify: %q is not a decimal amount", s)
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		digits := s[i+1:]
		if j := strings.IndexAny(digits, "eE"); j >= 0 {
			digits = digits[:j]
		}
		a.scale = len(digits)
	}
	return a, nil
}

// Rat is the exact value.
func (a Amount) Rat() *big.Rat { return new(big.Rat).Set(&a.r) }

// Float64 is the value as a float, for display or rough comparison.
func (a Amount) Float64() float64 {
	f, _ := a.r.Float64()
	return f
}

// IsZero reports whether the amount is zero.
func (a Amount) IsZero() bool { return a.r.Sign() == 0 }

// Add returns a + b, at the finer of the two scales.
func (a Amount) Add(b Amount) Amount {
	var out Amount
	out.r.Add(&a.r, &b.r)
	out.scale = max(a.scale, b.scale)
	return out
}

// Cmp compares a and b, returning -1, 0 or +1.
func (a Amount) Cmp(b Amount) int { return a.r.Cmp(&b.r) }

func (a Amount) String() string { return a.r.FloatString(a.scale) }

// Usage is what a run consumed, summed across every model call it made.
//
// Costs are kept per currency rather than as one number, because a workflow
// may call providers that price in different ones and adding those together
// would produce a figure that means nothing.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Latency          time.Duration
	// Costs maps a currency code to the amount spent in it.
	//
	// nil means nobody said — which is not the same as a run that cost
	// nothing, and reporting it as zero is how a budget check passes a run it
	// never measured. Dify's workflow_finished event carries token counts and
	// no price at all, so this is the common case rather than an edge one. An
	// empty, non-nil map is a reported zero. An amount that arrived with no
	// currency is kept under "".
	Costs map[string]Amount
}

// ErrCostUnknown is returned by TotalPrice when nothing reported a cost.
var ErrCostUnknown = errors.New("dify: no cost was reported")

// CostKnown reports whether anything reported a cost at all. False means no
// figure arrived, not that the run was free.
func (u Usage) CostKnown() bool { return u.Costs != nil }

// TotalPrice is the amount spent, when it was all in one currency, and that
// currency. ErrCostUnknown when nothing reported a cost; an error naming the
// currencies when several were involved, since there is no exchange rate here
// to combine them. A reported free run is a zero Amount and no error.
func (u Usage) TotalPrice() (Amount, string, error) {
	switch len(u.Costs) {
	case 0:
		if u.Costs == nil {
			return Amount{}, "", ErrCostUnknown
		}
		return Amount{}, "", nil
	case 1:
		for code, amount := range u.Costs {
			return amount, code, nil
		}
	}
	return Amount{}, "", fmt.Errorf("dify: the run spent in more than one currency (%s); read Usage.Costs", spent(u.Costs))
}

// IsZero reports whether nothing was consumed and no cost was reported.
func (u Usage) IsZero() bool { return u.TotalTokens == 0 && len(u.Costs) == 0 }

// Add sums two figures, keeping "nobody said" out of the total: unknown plus
// a number is that number, because adding a zero the server never reported
// would turn an unmeasured half into a measured one.
func (u Usage) Add(o Usage) Usage {
	var costs map[string]Amount
	if u.Costs != nil || o.Costs != nil {
		costs = make(map[string]Amount, len(u.Costs)+len(o.Costs))
		for code, amount := range u.Costs {
			costs[code] = amount
		}
		for code, amount := range o.Costs {
			costs[code] = costs[code].Add(amount)
		}
	}
	return Usage{
		PromptTokens:     u.PromptTokens + o.PromptTokens,
		CompletionTokens: u.CompletionTokens + o.CompletionTokens,
		TotalTokens:      u.TotalTokens + o.TotalTokens,
		Latency:          u.Latency + o.Latency,
		Costs:            costs,
	}
}

// mergedWith combines what was observed here with a server-reported total.
//
// Neither is complete: Dify's run total carries tokens and no price, while
// the per-node figures carry both but only for the nodes this client watched.
// So tokens come from the total when there is one, and the cost from
// whichever reported an amount — a run total of "0" is what Dify sends for a
// model it has no pricing for, and preferring it over node figures that were
// actually charged would report a billed run as free.
func (u Usage) mergedWith(total Usage) Usage {
	costs := total.Costs
	if len(total.Costs) == 0 && u.CostKnown() {
		costs = u.Costs
	}
	return Usage{
		PromptTokens:     firstNonZero(total.PromptTokens, u.PromptTokens),
		CompletionTokens: firstNonZero(total.CompletionTokens, u.CompletionTokens),
		TotalTokens:      firstNonZero(total.TotalTokens, u.TotalTokens),
		Latency:          firstNonZero(total.Latency, u.Latency),
		Costs:            costs,
	}
}

func (u Usage) String() string {
	if u.IsZero() {
		return "no model usage"
	}
	tokens := fmt.Sprintf("%d tokens (%d in / %d out)", u.TotalTokens, u.PromptTokens, u.CompletionTokens)
	switch {
	case u.Costs == nil:
		return tokens + " · cost not reported"
	case len(u.Costs) == 0:
		return tokens + " · free"
	}
	return tokens + " · " + spent(u.Costs)
}

// usageFrom reads tokens and cost out of Dify's execution_metadata, or any
// block spelled the same way.
func usageFrom(meta object, elapsed any) Usage {
	u := Usage{
		PromptTokens:     meta.int("prompt_tokens"),
		CompletionTokens: meta.int("completion_tokens"),
		TotalTokens:      meta.int("total_tokens"),
	}
	if secs, ok := asFloat(elapsed); ok {
		u.Latency = time.Duration(secs * float64(time.Second))
	}
	if meta.has("total_price") {
		u.Costs = map[string]Amount{}
		// A reported zero is a reported figure; only an absent price is
		// unknown. An amount with no currency beside it is still an amount.
		if amount, err := ParseAmount(meta.str("total_price")); err == nil && !amount.IsZero() {
			u.Costs[meta.str("currency")] = amount
		}
	}
	return u
}

func spent(costs map[string]Amount) string {
	codes := make([]string, 0, len(costs))
	for code := range costs {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, strings.TrimSpace(costs[code].String()+" "+code))
	}
	return strings.Join(parts, ", ")
}

func firstNonZero[T comparable](a, b T) T {
	var zero T
	if a != zero {
		return a
	}
	return b
}
