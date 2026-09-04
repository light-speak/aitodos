package experience

import "testing"

func TestInputNormalizesAndValidatesScope(t *testing.T) {
	input := Input{
		TaskID: " task-1 ", Title: " Git 同步 ", Summary: " 先同步目标分支 ",
		Guidance: "合并前检查 ahead/behind", Applicability: "涉及 worktree 同步时", ProjectWide: true,
	}.Normalized()
	if input.TaskID != "task-1" || input.Title != "Git 同步" {
		t.Fatalf("normalized input = %#v", input)
	}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	input.TopicID = "topic-1"
	if err := input.Validate(); err == nil {
		t.Fatal("同时绑定 Topic 和 Task 应被拒绝")
	}
}

func TestScoreRewardsEvidenceAndScopeWithoutUsingRecallCount(t *testing.T) {
	strong := Score(InputSignals{
		Relevance: 0.8, ScopeMatch: 1, Freshness: 0.9,
		VerificationCount: 3, SuccessCount: 3, FailureCount: 0, Pinned: true,
	})
	weak := Score(InputSignals{
		Relevance: 0.8, ScopeMatch: 0.6, Freshness: 0.9,
		VerificationCount: 0, SuccessCount: 0, FailureCount: 2,
	})
	if strong.Final <= weak.Final || strong.Utility <= weak.Utility {
		t.Fatalf("strong = %#v, weak = %#v", strong, weak)
	}
	if strong.Final < 0 || strong.Final > 1 || weak.Final < 0 || weak.Final > 1 {
		t.Fatalf("scores out of range: %#v %#v", strong, weak)
	}
}

func TestLexicalRelevanceSupportsChineseAndCodeTerms(t *testing.T) {
	matched := LexicalRelevance("修复 Git worktree 同步冲突", "Git worktree 同步", "先更新目标提交再继续")
	unrelated := LexicalRelevance("修复 Git worktree 同步冲突", "CSS 配色", "调整按钮颜色")
	if matched <= unrelated || matched == 0 {
		t.Fatalf("matched = %f, unrelated = %f", matched, unrelated)
	}
}
