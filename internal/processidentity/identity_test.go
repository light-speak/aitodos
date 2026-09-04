package processidentity

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
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

func TestTerminateGroupRejectsIdentityMismatch(t *testing.T) {
	if err := TerminateGroup(context.Background(), os.Getpid(), "wrong"); err == nil {
		t.Fatal("TerminateGroup() accepted mismatched process identity")
	}
}

func TestTerminateGroupStopsOwnedChildProcessGroup(t *testing.T) {
	if os.Getenv("ATS_PROCESS_IDENTITY_HELPER") == "1" {
		time.Sleep(time.Minute)
		return
	}
	command := exec.Command(os.Args[0], "-test.run=TestTerminateGroupStopsOwnedChildProcessGroup")
	command.Env = append(os.Environ(), "ATS_PROCESS_IDENTITY_HELPER=1")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	identity, err := Read(context.Background(), command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		t.Fatal(err)
	}
	if err := TerminateGroup(context.Background(), command.Process.Pid, identity); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
		t.Fatal("child process group did not exit")
	}
}
