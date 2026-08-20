// Package contextbuilder 以稳定顺序构建受 Token 预算约束的 Run Prompt。
package contextbuilder

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

// Chunk 是一个可独立取舍和去重的 Context 来源。
type Chunk struct {
	Source   string
	Content  string
	Required bool
	Priority int
}

// ManifestEntry 记录 Context 来源的采用结果。
type ManifestEntry struct {
	Source          string `json:"source"`
	SHA256          string `json:"sha256"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Reason          string `json:"reason,omitempty"`
}

// Manifest 记录 Run Prompt 的可重建来源和预算取舍。
type Manifest struct {
	BudgetTokens       int             `json:"budget_tokens"`
	EstimatedTokens    int             `json:"estimated_tokens"`
	RequiredOverBudget bool            `json:"required_over_budget"`
	Included           []ManifestEntry `json:"included"`
	Omitted            []ManifestEntry `json:"omitted"`
	Deduplicated       []ManifestEntry `json:"deduplicated"`
}

// Assemble 保留必需 Context，并从低优先级内容开始省略。
func Assemble(chunks []Chunk, budgetTokens int) (string, Manifest, error) {
	if budgetTokens < 1 {
		return "", Manifest{}, errors.New("context budget must be positive")
	}
	manifest := Manifest{BudgetTokens: budgetTokens, Included: []ManifestEntry{}, Omitted: []ManifestEntry{}, Deduplicated: []ManifestEntry{}}
	unique := deduplicate(chunks, &manifest)
	required, optional := partition(unique)
	sort.SliceStable(optional, func(left, right int) bool { return optional[left].Priority < optional[right].Priority })
	selected := make([]Chunk, 0, len(unique))
	for _, chunk := range required {
		manifest.EstimatedTokens += estimatedTokens(chunk.Content)
		selected = append(selected, chunk)
		manifest.Included = append(manifest.Included, entry(chunk, ""))
	}
	manifest.RequiredOverBudget = manifest.EstimatedTokens > budgetTokens
	for _, chunk := range optional {
		tokens := estimatedTokens(chunk.Content)
		if manifest.EstimatedTokens+tokens > budgetTokens {
			manifest.Omitted = append(manifest.Omitted, entry(chunk, "soft token budget exceeded"))
			continue
		}
		manifest.EstimatedTokens += tokens
		selected = append(selected, chunk)
		manifest.Included = append(manifest.Included, entry(chunk, ""))
	}
	return render(selected), manifest, nil
}

func deduplicate(chunks []Chunk, manifest *Manifest) []Chunk {
	seen := make(map[string]struct{}, len(chunks))
	result := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		chunk.Content = strings.TrimSpace(chunk.Content)
		if chunk.Content == "" {
			continue
		}
		hash := contentHash(chunk.Content)
		if _, exists := seen[hash]; exists {
			manifest.Deduplicated = append(manifest.Deduplicated, entry(chunk, "duplicate content hash"))
			continue
		}
		seen[hash] = struct{}{}
		result = append(result, chunk)
	}
	return result
}

func partition(chunks []Chunk) ([]Chunk, []Chunk) {
	required := make([]Chunk, 0)
	optional := make([]Chunk, 0)
	for _, chunk := range chunks {
		if chunk.Required {
			required = append(required, chunk)
		} else {
			optional = append(optional, chunk)
		}
	}
	return required, optional
}

func render(chunks []Chunk) string {
	var builder strings.Builder
	for _, chunk := range chunks {
		builder.WriteString("## ")
		builder.WriteString(chunk.Source)
		builder.WriteString("\n\n")
		builder.WriteString(chunk.Content)
		builder.WriteString("\n\n")
	}
	return strings.TrimSpace(builder.String()) + "\n"
}

func entry(chunk Chunk, reason string) ManifestEntry {
	return ManifestEntry{
		Source: chunk.Source, SHA256: contentHash(chunk.Content),
		EstimatedTokens: estimatedTokens(chunk.Content), Reason: reason,
	}
}

func contentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func estimatedTokens(content string) int {
	asciiRunes := 0
	nonASCIIRunes := 0
	for _, character := range content {
		if character <= 0x7f {
			asciiRunes++
		} else {
			nonASCIIRunes++
		}
	}
	return (asciiRunes+3)/4 + nonASCIIRunes
}
