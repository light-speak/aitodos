package processidentity

import (
	"context"
	"os"
	"testing"
)

func TestReadAndMatchCurrentProcess(t *testing.T) {
	identity, err := Read(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(context.Background(), os.Getpid(), identity) || Matches(context.Background(), os.Getpid(), "wrong") {
		t.Fatalf("identity %q did not fence current process", identity)
	}
}
