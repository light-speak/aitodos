package task

import "testing"

func TestCreateInputAcceptsOnlyP0ThroughP3(t *testing.T) {
	for _, priority := range []int{0, 1, 2, 3} {
		if err := (CreateInput{Title: "任务", Priority: priority}).Validate(); err != nil {
			t.Fatalf("priority P%d error = %v", priority, err)
		}
	}
	for _, priority := range []int{-1, 4} {
		if err := (CreateInput{Title: "任务", Priority: priority}).Validate(); err == nil {
			t.Fatalf("priority %d unexpectedly valid", priority)
		}
	}
}

func TestCreateInputRejectsUnknownTitleSource(t *testing.T) {
	input := CreateInput{Title: "任务", Priority: 1, TitleSource: TitleSource("EXTERNAL")}
	if err := input.Validate(); err == nil {
		t.Fatal("unknown title source unexpectedly valid")
	}
}
