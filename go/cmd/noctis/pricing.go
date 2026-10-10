package main

import (
	"sort"
	"strings"
)

type modelPrice struct {
	input, output, cacheWrite, cacheRead float64
}

type reportPrice struct {
	modelPrice
	longPromptPrice  modelPrice
	longPromptPriced bool
}

const longPromptTokens = 100000

var longPromptPrices = map[string]modelPrice{
	"haiku-5-5": {0.5, 2.5, 0.625, 0.05},
}

var builtinPrices = []struct {
	match string
	price modelPrice
}{
	{"fable-5-1", modelPrice{10, 50, 12.5, 0.25}},
	{"mythos-5-1", modelPrice{10, 50, 12.5, 0.25}},
	{"fable-5", modelPrice{10, 50, 12.5, 1}},
	{"mythos-5", modelPrice{10, 50, 12.5, 1}},
	{"opus-5-5", modelPrice{4, 20, 5, 0.2}},
	{"opus-5", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-8", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-7", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-6", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-5", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-1", modelPrice{15, 75, 18.75, 1.5}},
	{"opus-4", modelPrice{15, 75, 18.75, 1.5}},
	{"sonnet-5-5", modelPrice{2, 10, 2.5, 0.1}},
	{"sonnet-5", modelPrice{2, 10, 2.5, 0.2}},
	{"sonnet-4-6", modelPrice{3, 15, 3.75, 0.3}},
	{"sonnet-4-5", modelPrice{3, 15, 3.75, 0.3}},
	{"sonnet-4", modelPrice{3, 15, 3.75, 0.3}},
	{"3-7-sonnet", modelPrice{3, 15, 3.75, 0.3}},
	{"haiku-5-5", modelPrice{0.1, 0.5, 0.125, 0.01}},
	{"haiku-4-5", modelPrice{1, 5, 1.25, 0.1}},
	{"haiku-3-5", modelPrice{0.8, 4, 1, 0.08}},
	{"3-5-haiku", modelPrice{0.8, 4, 1, 0.08}},
}

func priceFor(cfg object, model string) (reportPrice, bool) {
	needle := strings.ToLower(model)
	if needle == "" {
		return reportPrice{}, false
	}
	listed, found := reportPrice{}, false
	for _, entry := range builtinPrices {
		if strings.Contains(needle, entry.match) || (len(needle) >= 4 && strings.Contains(entry.match, needle)) {
			listed.modelPrice, found = entry.price, true
			listed.longPromptPrice, listed.longPromptPriced = longPromptPrices[entry.match]
			break
		}
	}

	overrides := getMap(section(cfg, "report"), "pricing")
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		entry := toObject(overrides[key])
		if entry == nil || !strings.Contains(needle, strings.ToLower(key)) {
			continue
		}
		// A price the override leaves out stays the list price.
		return reportPrice{modelPrice: modelPrice{numberOr(entry, "input", listed.input), numberOr(entry, "output", listed.output), numberOr(entry, "cacheWrite", listed.cacheWrite), numberOr(entry, "cacheRead", listed.cacheRead)}}, true
	}
	return listed, found
}

type costParts struct {
	input, cacheWrite, cacheRead, output float64
}

func (parts costParts) total() float64 {
	return parts.input + parts.cacheWrite + parts.cacheRead + parts.output
}

func (parts costParts) plus(other costParts) costParts {
	return costParts{parts.input + other.input, parts.cacheWrite + other.cacheWrite, parts.cacheRead + other.cacheRead, parts.output + other.output}
}

func (parts costParts) minus(other costParts) costParts {
	return costParts{parts.input - other.input, parts.cacheWrite - other.cacheWrite, parts.cacheRead - other.cacheRead, parts.output - other.output}
}

func (bucket tokenBucket) cost(price reportPrice) float64 {
	return bucket.parts(price).total()
}

func (bucket tokenBucket) parts(price reportPrice) costParts {
	parts := bucket.partsAt(price.modelPrice)
	if long := bucket.longPrompt; long != nil && price.longPromptPriced {
		parts = parts.plus(long.partsAt(price.longPromptPrice)).minus(long.partsAt(price.modelPrice))
	}
	return parts
}

func (bucket tokenBucket) partsAt(price modelPrice) costParts {
	fiveMinute := bucket.cacheWrite - bucket.cacheWrite1h
	return costParts{
		input:      bucket.input * price.input / 1e6,
		cacheWrite: (fiveMinute*price.cacheWrite + bucket.cacheWrite1h*2*price.input) / 1e6,
		cacheRead:  bucket.cacheRead * price.cacheRead / 1e6,
		output:     bucket.output * price.output / 1e6,
	}
}

func formatUSD(value float64) string {
	switch {
	case value >= 100:
		return "$" + formatNumber(roundTo(value, 0))
	case value >= 1:
		return "$" + formatNumber(roundTo(value, 2))
	}
	return "$" + formatNumber(roundTo(value, 3))
}

func roundTo(value float64, digits int) float64 {
	scale := 1.0
	for i := 0; i < digits; i++ {
		scale *= 10
	}
	if value >= 0 {
		return float64(int64(value*scale+0.5)) / scale
	}
	return -float64(int64(-value*scale+0.5)) / scale
}
