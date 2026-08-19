package release

import (
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
