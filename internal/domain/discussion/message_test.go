package discussion

import "testing"

func TestCreateMessageInputNormalizesAndValidatesContent(t *testing.T) {
	input := CreateMessageInput{
		Content:       "  需要保留这个决定  ",
		LinkedTaskIDs: []string{" task-2 ", "task-1", "task-2", ""},
	}.Normalized()
	if input.Content != "需要保留这个决定" {
		t.Fatalf("normalized content = %q", input.Content)
	}
	if len(input.LinkedTaskIDs) != 2 || input.LinkedTaskIDs[0] != "task-2" || input.LinkedTaskIDs[1] != "task-1" {
		t.Fatalf("normalized linked task IDs = %#v", input.LinkedTaskIDs)
	}
	if err := input.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (CreateMessageInput{}).Validate(); err == nil {
		t.Fatal("Validate() error = nil, want required error")
	}
}

func TestCreateMessageInputLimitsLinkedTasks(t *testing.T) {
	input := CreateMessageInput{Content: "关联太多任务"}
	for index := 0; index < 21; index++ {
		input.LinkedTaskIDs = append(input.LinkedTaskIDs, string(rune('a'+index)))
	}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want linked task limit error")
	}
}
