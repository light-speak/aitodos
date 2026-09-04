package run

import (
	"strings"
	"testing"
)

func TestClosureNormalizesAndValidates(t *testing.T) {
	closure := Closure{
		StopReason: StopReasonGoalReached,
		Summary:    " 完成搜索接口 ",
		Completed:  []string{" 接口与索引 "},
		Verified:   []Verification{{Claim: " 搜索返回正确结果 ", Evidence: " go test ./internal/search "}},
		Unverified: []string{" 大规模数据性能 "},
	}
	if err := closure.Validate(); err != nil {
		t.Fatal(err)
	}
	normalized := closure.Normalized()
	if normalized.Summary != "完成搜索接口" || normalized.Completed[0] != "接口与索引" ||
		normalized.Verified[0].Evidence != "go test ./internal/search" {
		t.Fatalf("normalized = %#v", normalized)
	}
}

func TestClosureRejectsDishonestOrIncompleteReport(t *testing.T) {
	tests := []Closure{
		{},
		{StopReason: StopReasonGoalReached, Summary: "完成"},
		{StopReason: StopReasonEnvironmentBlocked, Summary: "环境缺失", Completed: []string{"分析完成"}},
		{StopReason: StopReasonGoalReached, Summary: "完成", Completed: []string{"实现"}, Verified: []Verification{{Claim: "已测试"}}},
	}
	for _, closure := range tests {
		if err := closure.Validate(); err == nil {
			t.Fatalf("closure %#v unexpectedly valid", closure)
		}
	}
}

func TestClosureMapsAgentStopReasonToRunStatus(t *testing.T) {
	if status := StatusForClosure(Closure{StopReason: StopReasonGoalReached}); status != StatusSucceeded {
		t.Fatalf("goal reached status = %q", status)
	}
	for _, reason := range []StopReason{StopReasonEnvironmentBlocked, StopReasonPolicyBlocked, StopReasonLimitReached} {
		if status := StatusForClosure(Closure{StopReason: reason}); status != StatusFailed {
			t.Fatalf("reason %q status = %q", reason, status)
		}
	}
}

func TestClosureRejectsAllInvalidBoundaries(t *testing.T) {
	tooMany := make([]string, 51)
	for index := range tooMany {
		tooMany[index] = "item"
	}
	tests := []Closure{
		{StopReason: "UNKNOWN", Summary: "未知", NextAction: "检查"},
		{StopReason: StopReasonGoalReached, Summary: strings.Repeat("结", 4001), Completed: []string{"完成"}},
		{StopReason: StopReasonGoalReached, Summary: "完成", Completed: tooMany},
		{StopReason: StopReasonGoalReached, Summary: "完成", Completed: []string{strings.Repeat("项", 1001)}},
		{StopReason: StopReasonEnvironmentBlocked, Summary: "阻塞", NextAction: strings.Repeat("步", 2001)},
		{StopReason: StopReasonGoalReached, Summary: "完成", Completed: []string{"实现"}, Verified: make([]Verification, 51)},
		{StopReason: StopReasonGoalReached, Summary: "完成", Completed: []string{"实现"}, Verified: []Verification{{Claim: "", Evidence: "命令"}}},
		{StopReason: StopReasonGoalReached, Summary: "完成", Completed: []string{"实现"}, Verified: []Verification{{Claim: strings.Repeat("声", 1001), Evidence: "命令"}}},
		{StopReason: StopReasonGoalReached, Summary: "完成", Completed: []string{"实现"}, Verified: []Verification{{Claim: "已测试", Evidence: strings.Repeat("证", 2001)}}},
	}
	for _, closure := range tests {
		if err := closure.Validate(); err == nil {
			t.Fatalf("closure %#v unexpectedly valid", closure)
		}
	}
}

func TestClosureNormalizesEmptyListItems(t *testing.T) {
	closure := Closure{
		RunID: " run ", StopReason: StopReasonGoalReached, Summary: " 完成 ",
		Completed: []string{"", " 实现 "}, Unverified: []string{" "}, RemainingRisks: []string{" 风险 "},
		NextAction: " 验收 ", Verified: []Verification{{Claim: " 声明 ", Evidence: " 证据 "}},
	}.Normalized()
	if closure.RunID != "run" || len(closure.Completed) != 1 || len(closure.Unverified) != 0 ||
		closure.RemainingRisks[0] != "风险" || closure.NextAction != "验收" || closure.Verified[0].Claim != "声明" {
		t.Fatalf("normalized closure = %#v", closure)
	}
}
