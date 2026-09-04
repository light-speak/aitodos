package buildinfo

import "testing"

func TestCurrentAndString(t *testing.T) {
	originalVersion, originalCommit, originalDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = originalVersion, originalCommit, originalDate })
	Version, Commit, Date = "v1.2.3", "abcdef12", "2026-09-02T00:00:00Z"

	current := Current()
	if current.Version != Version || current.Commit != Commit || current.Date != Date {
		t.Fatalf("Current() = %#v", current)
	}
	want := "ats v1.2.3 (commit abcdef12, built 2026-09-02T00:00:00Z)"
	if got := String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
