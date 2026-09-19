package main

import (
	"sort"
	"strings"
)

type modelPrice struct {
	input, output, cacheWrite, cacheRead float64
}

var builtinPrices = []struct {
	match string
	price modelPrice
}{
	{"fable-5-1", modelPrice{10, 50, 12.5, 0.25}},
	{"mythos-5-1", modelPrice{10, 50, 12.5, 0.25}},
	{"fable-5", modelPrice{10, 50, 12.5, 1}},
	{"mythos-5", modelPrice{10, 50, 12.5, 1}},
	{"opus-5", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-8", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-7", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-6", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-5", modelPrice{5, 25, 6.25, 0.5}},
	{"opus-4-1", modelPrice{15, 75, 18.75, 1.5}},
	{"opus-4", modelPrice{15, 75, 18.75, 1.5}},
	{"sonnet-5", modelPrice{2, 10, 2.5, 0.2}},
	{"sonnet-4-6", modelPrice{3, 15, 3.75, 0.3}},
	{"sonnet-4-5", modelPrice{3, 15, 3.75, 0.3}},
	{"sonnet-4", modelPrice{3, 15, 3.75, 0.3}},
	{"haiku-4-5", modelPrice{1, 5, 1.25, 0.1}},
	{"haiku-3-5", modelPrice{0.8, 4, 1, 0.08}},
}

func priceFor(cfg object, model string) (modelPrice, bool) {
	needle := strings.ToLower(model)
	if needle == "" {
		return modelPrice{}, false
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
		return modelPrice{numberOr(entry, "input", 0), numberOr(entry, "output", 0), numberOr(entry, "cacheWrite", 0), numberOr(entry, "cacheRead", 0)}, true
	}
	for _, entry := range builtinPrices {
		if strings.Contains(needle, entry.match) || (len(needle) >= 4 && strings.Contains(entry.match, needle)) {
			return entry.price, true
		}
	}
	return modelPrice{}, false
}

func (bucket tokenBucket) cost(price modelPrice) float64 {
	return (bucket.input*price.input + bucket.output*price.output + bucket.cacheWrite*price.cacheWrite + bucket.cacheRead*price.cacheRead) / 1e6
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
