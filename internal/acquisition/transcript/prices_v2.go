package transcript

const (
	// PriceTableVersion is persisted in every encrypted transcript payload.
	// Values are standard direct API-equivalent USD list prices per MTok.
	PriceTableVersion = "belay.local.prices.v2"
	// PriceTableEffectiveDate records when this in-repo table became effective.
	PriceTableEffectiveDate = "2026-09-09"
	// PriceTableSource documents the public list-price basis without requiring
	// Belay Local to make a network request.
	PriceTableSource = "Standard direct API-equivalent list price, observed 2026-09-09"
)

type modelPrice struct {
	Input        float64
	Output       float64
	CacheRead    float64
	CacheWrite5M float64
	CacheWrite1H float64
	Long         *modelPrice
}

var pricesV2 = map[string]modelPrice{
	"claude-opus-4-6": {
		Input:        5,
		Output:       25,
		CacheRead:    0.50,
		CacheWrite5M: 6.25,
		CacheWrite1H: 10,
	},
	"claude-opus-4-7": {
		Input: 5, Output: 25, CacheRead: 0.50,
		CacheWrite5M: 6.25, CacheWrite1H: 10,
	},
	"claude-opus-4-8": {
		Input: 5, Output: 25, CacheRead: 0.50,
		CacheWrite5M: 6.25, CacheWrite1H: 10,
	},
	"claude-opus-5": {
		Input: 6, Output: 30, CacheRead: 0.60,
		CacheWrite5M: 7.50, CacheWrite1H: 12,
	},
	"claude-fable-5": {
		Input: 6, Output: 30, CacheRead: 0.60,
		CacheWrite5M: 7.50, CacheWrite1H: 12,
	},
	"claude-haiku-4-5-20251001": {
		Input:        1,
		Output:       5,
		CacheRead:    0.10,
		CacheWrite5M: 1.25,
		CacheWrite1H: 2,
	},
	"openai.gpt-5.6-sol": {
		Input: 4, Output: 20, CacheRead: 0.40,
		Long: &modelPrice{Input: 8, Output: 30, CacheRead: 0.80},
	},
	"openai.gpt-5.6-luna": {
		Input: 0.25, Output: 2, CacheRead: 0.025,
	},
	"openai.gpt-5.4": {
		Input: 2.50, Output: 15, CacheRead: 0.25,
		Long: &modelPrice{Input: 5, Output: 22.50, CacheRead: 0.50},
	},
	"openai.gpt-6-astra": {
		Input: 4, Output: 30, CacheRead: 0.40,
		Long: &modelPrice{Input: 8, Output: 45, CacheRead: 0.80},
	},
}

// PricingUsage is the public, bounded input for evaluating one harness usage
// record against Belay's versioned in-repo price table.
type PricingUsage struct {
	InputTokens       *int64
	OutputTokens      *int64
	CacheReadTokens   *int64
	CacheWriteTokens  *int64
	InputIncludesRead bool
}

// EstimateCostUSD returns nil when the model is not present in the versioned
// table. It never guesses a price.
func EstimateCostUSD(model string, value PricingUsage) *float64 {
	return calculateCost(model, usage{
		InputTokens:       value.InputTokens,
		OutputTokens:      value.OutputTokens,
		CacheReadTokens:   value.CacheReadTokens,
		CacheWriteTokens:  value.CacheWriteTokens,
		InputIncludesRead: value.InputIncludesRead,
	})
}

func calculateCost(model string, value usage) *float64 {
	price, ok := pricesV2[model]
	if !ok {
		return nil
	}
	if count(value.InputTokens) > 272_000 && price.Long != nil {
		price = *price.Long
	}
	input := count(value.InputTokens)
	cacheRead := count(value.CacheReadTokens)
	if value.InputIncludesRead {
		input -= cacheRead
		if input < 0 {
			input = 0
		}
	}
	cache5M := 0.0
	cache1H := 0.0
	if !value.InputIncludesRead {
		cache5M = count(value.CacheWrite5M)
		cache1H = count(value.CacheWrite1H)
		cacheWrite := count(value.CacheWriteTokens)
		if value.CacheWrite5M == nil && value.CacheWrite1H == nil {
			cache5M = cacheWrite
		} else if remainder := cacheWrite - cache5M - cache1H; remainder > 0 {
			cache5M += remainder
		}
	}
	total := input*price.Input +
		count(value.OutputTokens)*price.Output +
		cacheRead*price.CacheRead +
		cache5M*price.CacheWrite5M +
		cache1H*price.CacheWrite1H
	cost := total / 1_000_000
	return &cost
}

func count(value *int64) float64 {
	if value == nil {
		return 0
	}
	return float64(*value)
}
