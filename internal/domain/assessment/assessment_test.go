package assessment

import "testing"

func TestCalculateDerivesComplexityAndAutonomy(t *testing.T) {
	tests := []struct {
		name       string
		scores     DimensionScores
		complexity ComplexityLevel
		autonomy   AutonomyLevel
	}{
		{name: "fully autonomous", scores: DimensionScores{}, complexity: ComplexityC1, autonomy: AutonomyA3},
		{name: "routine with review", scores: DimensionScores{TechnicalComplexity: 2, ValidationBurden: 2, HumanDependency: 1}, complexity: ComplexityC2, autonomy: AutonomyA2},
		{name: "human coordination", scores: DimensionScores{TechnicalComplexity: 2, RequirementUncertainty: 2, ChangeScope: 2, ValidationBurden: 3, HumanDependency: 3, RiskAndReversibility: 2}, complexity: ComplexityC3, autonomy: AutonomyA1},
		{name: "manual dependency", scores: DimensionScores{TechnicalComplexity: 4, RequirementUncertainty: 2, ChangeScope: 3, ValidationBurden: 3, HumanDependency: 4, RiskAndReversibility: 2}, complexity: ComplexityC4, autonomy: AutonomyA0},
		{name: "highest complexity", scores: DimensionScores{TechnicalComplexity: 4, RequirementUncertainty: 4, ChangeScope: 4, ValidationBurden: 4, HumanDependency: 4, RiskAndReversibility: 4}, complexity: ComplexityC5, autonomy: AutonomyA0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Calculate(test.scores)
			if err != nil {
				t.Fatal(err)
			}
			if result.Complexity != test.complexity || result.Autonomy != test.autonomy {
				t.Fatalf("Calculate() = %#v, want %s/%s", result, test.complexity, test.autonomy)
			}
		})
	}
}

func TestInputRejectsInvalidDimensionScore(t *testing.T) {
	input := Input{
		SuggestedTitle: "评估任务复杂度",
		Scores:         DimensionScores{TechnicalComplexity: 5},
		Confidence:     0.8,
		Rationale:      "需要跨模块修改",
	}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestInputNormalizesAndRejectsIncompleteAgentOutput(t *testing.T) {
	valid := Input{SuggestedTitle: " 标题 ", Confidence: 1, Rationale: " 原因 ", Assumptions: []string{" 假设 ", " "}, SplitRecommended: true, SplitRationale: " 拆分 "}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	normalized := valid.Normalized()
	if normalized.SuggestedTitle != "标题" || len(normalized.Assumptions) != 1 || normalized.SplitRationale != "拆分" {
		t.Fatalf("normalized = %#v", normalized)
	}
	tooManyAssumptions := make([]string, 21)
	for index := range tooManyAssumptions {
		tooManyAssumptions[index] = "假设"
	}
	for _, input := range []Input{
		{SuggestedTitle: " ", Rationale: "原因"},
		{SuggestedTitle: "标题", Confidence: -1, Rationale: "原因"},
		{SuggestedTitle: "标题", Confidence: 1, Rationale: " "},
		{SuggestedTitle: "标题", Confidence: 1, Rationale: "原因", Assumptions: tooManyAssumptions},
		{SuggestedTitle: "标题", Confidence: 1, Rationale: "原因", SplitRecommended: true},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("input %#v unexpectedly valid", input)
		}
	}
}
