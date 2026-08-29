package taskfeedback

import "testing"

func TestCreateInputValidatesExplicitIntent(t *testing.T) {
	valid := CreateInput{Intent: IntentDiscuss}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (CreateInput{Intent: Intent("AUTO")}).Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid intent")
	}
}

func TestStatusIsTerminal(t *testing.T) {
	if StatusQueued.Terminal() || StatusRunning.Terminal() {
		t.Fatal("queued or running feedback must not be terminal")
	}
	if !StatusAnswered.Terminal() || !StatusApplied.Terminal() || !StatusFailed.Terminal() {
		t.Fatal("answered, applied and failed feedback must be terminal")
	}
}
