package task

import (
	"errors"
	"testing"
)

func TestTransitionAllowsDocumentedCommands(t *testing.T) {
	tests := []struct {
		name    string
		current Status
		command Command
		want    Status
	}{
		{name: "queue backlog", current: StatusBacklog, command: CommandQueue, want: StatusReady},
		{name: "claim ready", current: StatusReady, command: CommandClaimRun, want: StatusRunning},
		{name: "succeed run", current: StatusRunning, command: CommandRunSucceeded, want: StatusReview},
		{name: "fail run", current: StatusRunning, command: CommandRunFailed, want: StatusBlocked},
		{name: "cancel run", current: StatusRunning, command: CommandCancelRun, want: StatusCancelled},
		{name: "accept task", current: StatusReview, command: CommandAccept, want: StatusAccepted},
		{name: "reject task", current: StatusReview, command: CommandReject, want: StatusChangesRequested},
		{name: "queue changes", current: StatusChangesRequested, command: CommandQueue, want: StatusReady},
		{name: "retry blocked", current: StatusBlocked, command: CommandRetry, want: StatusReady},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Transition(test.current, test.command)
			if err != nil {
				t.Fatalf("Transition() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Transition() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTransitionRejectsEveryUndocumentedCombination(t *testing.T) {
	allowed := map[[2]string]bool{
		{string(StatusBacklog), string(CommandQueue)}:          true,
		{string(StatusReady), string(CommandClaimRun)}:         true,
		{string(StatusRunning), string(CommandRunSucceeded)}:   true,
		{string(StatusRunning), string(CommandRunFailed)}:      true,
		{string(StatusRunning), string(CommandCancelRun)}:      true,
		{string(StatusReview), string(CommandAccept)}:          true,
		{string(StatusReview), string(CommandReject)}:          true,
		{string(StatusChangesRequested), string(CommandQueue)}: true,
		{string(StatusBlocked), string(CommandRetry)}:          true,
	}

	for _, status := range AllStatuses() {
		for _, command := range AllCommands() {
			if allowed[[2]string{string(status), string(command)}] {
				continue
			}
			got, err := Transition(status, command)
			if err == nil {
				t.Errorf("Transition(%q, %q) = %q, want error", status, command, got)
				continue
			}
			var transitionErr *TransitionError
			if !errors.As(err, &transitionErr) {
				t.Errorf("Transition(%q, %q) error = %T, want *TransitionError", status, command, err)
			}
		}
	}
}

func TestValidateCreateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   CreateInput
		wantErr bool
	}{
		{name: "valid", input: CreateInput{Title: "实现任务看板"}},
		{name: "description only", input: CreateInput{Description: "实现任务看板"}},
		{name: "empty content", input: CreateInput{Title: "  \n ", Description: " \n "}, wantErr: true},
		{name: "title too long", input: CreateInput{Title: string(make([]byte, 201))}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.input.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}

	derived := (CreateInput{Description: "  修复移动端布局溢出。\n需要覆盖回归测试。 "}).Normalized()
	if derived.Title != "修复移动端布局溢出。" {
		t.Fatalf("derived title = %q", derived.Title)
	}
}
