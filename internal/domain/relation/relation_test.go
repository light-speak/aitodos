package relation

import "testing"

func TestRelationInputsNormalizeAndValidate(t *testing.T) {
	taskInput := LinkTaskInput{TaskID: " task-1 ", Type: "blocks"}
	if normalized := taskInput.Normalized(); normalized.TaskID != "task-1" || normalized.Type != TypeBlocks {
		t.Fatalf("task normalized = %#v", normalized)
	}
	if err := taskInput.Normalized().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (LinkTaskInput{}).Validate(); err == nil {
		t.Fatal("empty task relation accepted")
	}
	defaultType := LinkTaskInput{TaskID: "task-2"}.Normalized()
	if defaultType.Type != TypeRelatesTo || defaultType.Validate() != nil {
		t.Fatalf("default relation = %#v", defaultType)
	}
	if err := (LinkTaskInput{TaskID: "task", Type: "UNKNOWN"}).Validate(); err == nil {
		t.Fatal("invalid task relation type accepted")
	}

	topicInput := LinkTopicInput{TopicID: " topic-1 "}
	if normalized := topicInput.Normalized(); normalized.TopicID != "topic-1" {
		t.Fatalf("topic normalized = %#v", normalized)
	}
	if err := topicInput.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (LinkTopicInput{}).Validate(); err == nil {
		t.Fatal("empty topic relation accepted")
	}
}
