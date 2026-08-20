package contextbuilder

import (
	"strings"
	"testing"
)

func TestAssemblePreservesRequiredContextAndOmitsLowPriority(t *testing.T) {
	chunks := []Chunk{
		{Source: "system", Content: strings.Repeat("S", 400), Required: true, Priority: 0},
		{Source: "acceptance", Content: strings.Repeat("A", 400), Required: true, Priority: 0},
		{Source: "recent", Content: strings.Repeat("R", 800), Priority: 30},
		{Source: "archive", Content: strings.Repeat("H", 800), Priority: 50},
	}
	prompt, manifest, err := Assemble(chunks, 300)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, strings.Repeat("S", 100)) || !strings.Contains(prompt, strings.Repeat("A", 100)) {
		t.Fatal("required context was omitted")
	}
	if strings.Contains(prompt, strings.Repeat("H", 100)) {
		t.Fatal("lowest priority context was included over budget")
	}
	if len(manifest.Omitted) == 0 || manifest.EstimatedTokens > 300 {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestAssembleTreatsBudgetAsSoftLimitForRequiredContext(t *testing.T) {
	chunks := []Chunk{
		{Source: "system", Content: strings.Repeat("S", 800), Required: true},
		{Source: "acceptance", Content: strings.Repeat("A", 800), Required: true},
		{Source: "archive", Content: strings.Repeat("H", 400), Priority: 50},
	}
	prompt, manifest, err := Assemble(chunks, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, strings.Repeat("S", 100)) || !strings.Contains(prompt, strings.Repeat("A", 100)) {
		t.Fatal("required context was omitted by the soft budget")
	}
	if !manifest.RequiredOverBudget || manifest.EstimatedTokens <= manifest.BudgetTokens {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Omitted) != 1 || manifest.Omitted[0].Source != "archive" {
		t.Fatalf("omitted = %#v", manifest.Omitted)
	}
}

func TestAssembleDeduplicatesStableContentHash(t *testing.T) {
	chunks := []Chunk{
		{Source: "one", Content: "相同内容", Required: true},
		{Source: "two", Content: "相同内容", Required: true},
	}
	prompt, manifest, err := Assemble(chunks, 100)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(prompt, "相同内容") != 1 || len(manifest.Deduplicated) != 1 {
		t.Fatalf("prompt = %q, manifest = %#v", prompt, manifest)
	}
}

func TestEstimatedTokensDoesNotTreatChineseAsASCII(t *testing.T) {
	if got := estimatedTokens("数据库迁移需要人工确认"); got < 10 {
		t.Fatalf("estimatedTokens(chinese) = %d, want conservative multilingual estimate", got)
	}
	if got := estimatedTokens("abcdefghijklmnop"); got != 4 {
		t.Fatalf("estimatedTokens(ascii) = %d, want 4", got)
	}
}
