package release

import (
	"fmt"
	"strings"
	"testing"
)

func TestCreateInputNormalizesSemanticVersion(t *testing.T) {
	input := CreateInput{Version: " v1.2.3-beta.1 ", SourceBranch: " main "}.Normalized()
	if input.Version != "1.2.3-beta.1" || input.TagName() != "v1.2.3-beta.1" || input.SourceBranch != "main" {
		t.Fatalf("normalized input = %#v, tag = %q", input, input.TagName())
	}
	if err := input.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCreateInputRejectsInvalidSemanticVersions(t *testing.T) {
	for _, version := range []string{"", "1", "1.2", "01.2.3", "1.2.3-", "release-1.2.3"} {
		input := CreateInput{Version: version, SourceBranch: "main"}
		if err := input.Validate(); err == nil {
			t.Errorf("Validate(%q) error = nil", version)
		}
	}
	tooLong := CreateInput{Version: "1.0.0+" + strings.Repeat("a", 101), SourceBranch: "main"}
	if err := tooLong.Validate(); err == nil {
		t.Error("Validate() accepted an impractically long Git tag")
	}
}

func TestCreateInputNormalizesTaskIDsAndRequiresBranch(t *testing.T) {
	input := CreateInput{Version: "1.0.0", SourceBranch: " main ", TaskIDs: []string{" task-1 ", "", "task-1", "task-2"}}.Normalized()
	if len(input.TaskIDs) != 2 || input.TaskIDs[0] != "task-1" || input.TaskIDs[1] != "task-2" {
		t.Fatalf("task ids = %#v", input.TaskIDs)
	}
	if err := (CreateInput{Version: "1.0.0"}).Validate(); err == nil {
		t.Fatal("missing branch unexpectedly valid")
	}
	taskIDs := make([]string, 201)
	for index := range taskIDs {
		taskIDs[index] = fmt.Sprintf("task-%d", index)
	}
	if err := (CreateInput{Version: "1.0.0", SourceBranch: "main", TaskIDs: taskIDs}).Validate(); err == nil {
		t.Fatal("too many tasks unexpectedly valid")
	}
}
