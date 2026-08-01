package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Standard-tier USD per 1M tokens (OpenAI image generation pricing page).
// API returns usage tokens only; dollar cost is estimated from these rates.
var pricingPer1M = map[string]map[string]float64{
	"gpt-image-2": {
		"text_input": 5.0, "image_input": 8.0, "image_output": 30.0, "text_output": 0.0,
	},
	"gpt-image-1.5": {
		"text_input": 5.0, "image_input": 8.0, "image_output": 32.0, "text_output": 10.0,
	},
	"chatgpt-image-latest": {
		"text_input": 5.0, "image_input": 8.0, "image_output": 32.0, "text_output": 10.0,
	},
	"gpt-image-1": {
		"text_input": 5.0, "image_input": 10.0, "image_output": 40.0, "text_output": 0.0,
	},
	"gpt-image-1-mini": {
		"text_input": 2.0, "image_input": 2.5, "image_output": 8.0, "text_output": 0.0,
	},
}

// UsageSummary holds token usage and an estimated USD cost.
type UsageSummary struct {
	InputTokens  int
	OutputTokens int
	TextInput    int
	ImageInput   int
	ImageOutput  int
	TextOutput   int
	CostUSD      *float64
	Model        string
}

func pricingFor(model string) map[string]float64 {
	model = strings.TrimSpace(strings.ToLower(model))
	if rates, ok := pricingPer1M[model]; ok {
		return rates
	}
	for key, rates := range pricingPer1M {
		if strings.HasPrefix(model, key) {
			return rates
		}
	}
	return pricingPer1M["gpt-image-2"]
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case float32:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// SummarizeUsage pulls token usage from an Images API response and estimates USD cost.
func SummarizeUsage(result *ImageResult, model string) UsageSummary {
	summary := UsageSummary{Model: model}
	if result == nil || result.Usage == nil {
		return summary
	}
	usage := result.Usage
	summary.InputTokens = asInt(usage["input_tokens"])
	summary.OutputTokens = asInt(usage["output_tokens"])

	if inDet, ok := usage["input_tokens_details"].(map[string]any); ok {
		summary.TextInput = asInt(inDet["text_tokens"])
		summary.ImageInput = asInt(inDet["image_tokens"])
	} else {
		summary.TextInput = summary.InputTokens
	}
	if outDet, ok := usage["output_tokens_details"].(map[string]any); ok {
		summary.ImageOutput = asInt(outDet["image_tokens"])
		summary.TextOutput = asInt(outDet["text_tokens"])
	} else {
		summary.ImageOutput = summary.OutputTokens
	}

	rates := pricingFor(model)
	const million = 1_000_000.0
	cost := (float64(summary.TextInput)*rates["text_input"] +
		float64(summary.ImageInput)*rates["image_input"] +
		float64(summary.ImageOutput)*rates["image_output"] +
		float64(summary.TextOutput)*rates["text_output"]) / million
	summary.CostUSD = &cost
	return summary
}

// FormatLine matches the Python CLI style:
// ~$0.0531 est. · tokens in=18 out=1760 (text_in=18, img_out=1760) · 41s
func (u UsageSummary) FormatLine(elapsed time.Duration) string {
	var parts []string
	if u.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("~$%.4f est.", *u.CostUSD))
	}
	if u.InputTokens != 0 || u.OutputTokens != 0 {
		parts = append(parts, fmt.Sprintf("tokens in=%d out=%d", u.InputTokens, u.OutputTokens))
		var detail []string
		if u.TextInput != 0 {
			detail = append(detail, fmt.Sprintf("text_in=%d", u.TextInput))
		}
		if u.ImageInput != 0 {
			detail = append(detail, fmt.Sprintf("img_in=%d", u.ImageInput))
		}
		if u.ImageOutput != 0 {
			detail = append(detail, fmt.Sprintf("img_out=%d", u.ImageOutput))
		}
		if u.TextOutput != 0 {
			detail = append(detail, fmt.Sprintf("text_out=%d", u.TextOutput))
		}
		if len(detail) > 0 {
			parts = append(parts, "("+strings.Join(detail, ", ")+")")
		}
	}
	if elapsed > 0 {
		parts = append(parts, fmt.Sprintf("%.0fs", elapsed.Seconds()))
	}
	return strings.Join(parts, " · ")
}
