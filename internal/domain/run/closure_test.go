package run

import "testing"

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
