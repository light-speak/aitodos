package topic

import (
	"errors"
	"strings"
	"testing"
)

func TestTransitionAllowsDocumentedCommands(t *testing.T) {
	tests := []struct {
		name    string
		current Status
		command Command
		want    Status
	}{
		{name: "request clarification", current: StatusOpen, command: CommandRequestClarification, want: StatusNeedsClarification},
		{name: "answer clarification", current: StatusNeedsClarification, command: CommandAnswerClarification, want: StatusOpen},
		{name: "submit plan", current: StatusOpen, command: CommandSubmitPlan, want: StatusPlanReview},
		{name: "approve plan", current: StatusPlanReview, command: CommandApprovePlan, want: StatusPlanned},
		{name: "request plan changes", current: StatusPlanReview, command: CommandRequestPlanChanges, want: StatusOpen},
		{name: "close open topic", current: StatusOpen, command: CommandClose, want: StatusClosed},
		{name: "close planned topic", current: StatusPlanned, command: CommandClose, want: StatusClosed},
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

func TestTransitionRejectsUndocumentedCombination(t *testing.T) {
	got, err := Transition(StatusClosed, CommandSubmitPlan)
	if err == nil {
		t.Fatalf("Transition() = %q, want error", got)
	}
	var transitionErr *TransitionError
	if !errors.As(err, &transitionErr) {
		t.Fatalf("Transition() error = %T, want *TransitionError", err)
	}
	if transitionErr.Error() == "" {
		t.Fatal("TransitionError.Error() returned an empty message")
	}
}

func TestStateCatalogsContainEveryDeclaredValue(t *testing.T) {
	if got := AllStatuses(); len(got) != 5 || got[0] != StatusOpen || got[len(got)-1] != StatusClosed {
		t.Fatalf("AllStatuses() = %#v", got)
	}
	if got := AllCommands(); len(got) != 6 || got[0] != CommandRequestClarification || got[len(got)-1] != CommandClose {
		t.Fatalf("AllCommands() = %#v", got)
	}
}

func TestCreateInputValidationAndNormalization(t *testing.T) {
	tests := []struct {
		name    string
		input   CreateInput
		wantErr bool
	}{
		{name: "valid", input: CreateInput{Title: "讨论需求边界"}},
		{name: "description only", input: CreateInput{Description: "讨论需求边界"}},
		{name: "empty content", input: CreateInput{Title: " \n ", Description: " \n "}, wantErr: true},
		{name: "title too long", input: CreateInput{Title: strings.Repeat("界", 201)}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.input.Validate(); (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}

	normalized := (CreateInput{Title: " 需求讨论 ", Description: " 说明 "}).Normalized()
	if normalized.Title != "需求讨论" || normalized.Description != "说明" {
		t.Fatalf("Normalized() = %#v", normalized)
	}
	derived := (CreateInput{Description: "  希望先完善 Topic 讨论体验。\n然后再做搜索。 "}).Normalized()
	if derived.Title != "希望先完善 Topic 讨论体验。" {
		t.Fatalf("derived title = %q", derived.Title)
	}
}
