package task

import "testing"

func TestReviewInputValidation(t *testing.T) {
	if err := (ReviewInput{Decision: ReviewAccepted}).Validate(); err != nil {
		t.Fatalf("accepted review error = %v", err)
	}
	if err := (ReviewInput{Decision: ReviewRejected, Comment: "需要补充测试"}).Validate(); err != nil {
		t.Fatalf("rejected review error = %v", err)
	}
	for _, input := range []ReviewInput{
		{Decision: ReviewRejected},
		{Decision: "UNKNOWN"},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("Validate(%#v) error = nil", input)
		}
	}
}
