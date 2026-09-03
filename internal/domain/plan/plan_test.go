package plan

import "testing"

func TestRevisionInputValidate(t *testing.T) {
	input := RevisionInput{Summary: " 搜索方案 ", Drafts: []TaskDraftInput{{
		Title: " 建立索引 ", Priority: 1,
		TestCases: []TestCaseInput{{Title: " 可以按类型筛选 ", Required: true}},
	}}}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	normalized := input.Normalized()
	if normalized.Summary != "搜索方案" || normalized.Drafts[0].Title != "建立索引" {
		t.Fatalf("normalized = %#v", normalized)
	}
}

func TestRevisionInputRejectsMissingDrafts(t *testing.T) {
	if err := (RevisionInput{Summary: "方案"}).Validate(); err == nil {
		t.Fatal("Validate() should reject an empty plan")
	}
}

func TestReviewInputRejectsBlankChangesComment(t *testing.T) {
	input := ReviewInput{ExpectedTopicVersion: 2, RevisionID: "revision-1", Comment: "  "}
	if err := input.ValidateChangesRequest(); err == nil {
		t.Fatal("ValidateChangesRequest() should reject a blank comment")
	}
}

func TestPlanningResultNormalizesAndValidatesReply(t *testing.T) {
	result := PlanningResult{
		Reply: " 方案可以进入审核 ",
		Readiness: &ReadinessAssessment{
			Status: ReadinessReadyForReview, Confidence: 0.8,
			Assumptions:  []string{" 使用现有数据库 "},
			Alternatives: []Alternative{{Title: " 全量重建 ", Tradeoff: " 实现简单但停顿更长 "}},
		},
		Plan: &RevisionInput{Summary: " 方案 ", Drafts: []TaskDraftInput{{Title: " Task ", TestCases: []TestCaseInput{{Title: " 测试 "}}}}},
	}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	normalized := result.Normalized()
	if normalized.Reply != "方案可以进入审核" || normalized.Plan == nil || normalized.Plan.Drafts[0].TestCases[0].Title != "测试" ||
		normalized.Readiness == nil || normalized.Readiness.Assumptions[0] != "使用现有数据库" ||
		normalized.Readiness.Alternatives[0].Title != "全量重建" {
		t.Fatalf("normalized = %#v", normalized)
	}
	if err := (PlanningResult{}).Validate(); err == nil {
		t.Fatal("blank planning reply unexpectedly valid")
	}
}

func TestPlanningResultRequiresHonestReadiness(t *testing.T) {
	planInput := &RevisionInput{Summary: "方案", Drafts: []TaskDraftInput{{Title: "Task"}}}
	tests := []PlanningResult{
		{Reply: "缺少判断", Plan: planInput},
		{Reply: "仍需讨论", Readiness: &ReadinessAssessment{Status: ReadinessNeedsDiscussion, Confidence: 0.7}},
		{Reply: "错误收敛", Readiness: &ReadinessAssessment{Status: ReadinessNeedsDiscussion, Confidence: 0.7, OpenQuestions: []string{"部署目标"}}, Plan: planInput},
		{Reply: "仍有阻塞项", Readiness: &ReadinessAssessment{Status: ReadinessReadyForReview, Confidence: 0.7, OpenQuestions: []string{"数据保留期"}}, Plan: planInput},
	}
	for _, result := range tests {
		if err := result.Validate(); err == nil {
			t.Fatalf("result %#v unexpectedly valid", result)
		}
	}

	discussion := PlanningResult{
		Reply: "先确认部署目标",
		Readiness: &ReadinessAssessment{
			Status: ReadinessNeedsDiscussion, Confidence: 0.7,
			OpenQuestions: []string{"部署目标是本地还是公网"},
		},
	}
	if err := discussion.Validate(); err != nil {
		t.Fatalf("discussion result should be valid: %v", err)
	}
}

func TestTaskDraftRejectsInvalidFields(t *testing.T) {
	for _, draft := range []TaskDraftInput{
		{},
		{Title: "Task", Priority: 4},
		{Title: "Task", TestCases: []TestCaseInput{{Title: " "}}},
	} {
		if err := draft.Validate(); err == nil {
			t.Fatalf("draft %#v unexpectedly valid", draft)
		}
	}
}
