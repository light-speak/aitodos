package approvalrequest

import "testing"

func TestCreateInputValidationAndDecisionLookup(t *testing.T) {
	valid := CreateInput{
		ExternalRequestID: " request-1 ", Kind: KindNetwork, Host: " example.com ",
		Available: []Decision{DecisionAcceptOnce, DecisionDecline},
	}
	normalized := valid.Normalized()
	if normalized.ExternalRequestID != "request-1" || normalized.Host != "example.com" {
		t.Fatalf("normalized = %#v", normalized)
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	request := Request{Available: valid.Available}
	if !request.Allows(DecisionDecline) || request.Allows(DecisionAcceptSession) {
		t.Fatalf("decision lookup failed: %#v", request.Available)
	}

	invalid := []CreateInput{
		{},
		{ExternalRequestID: "request-1", Kind: "UNKNOWN", Available: []Decision{DecisionDecline}},
		{ExternalRequestID: "request-1", Kind: KindCommand},
		{ExternalRequestID: "request-1", Kind: KindCommand, Available: []Decision{DecisionDecline, DecisionDecline}},
		{ExternalRequestID: "request-1", Kind: KindCommand, Available: []Decision{"UNKNOWN"}},
	}
	for _, input := range invalid {
		if err := input.Validate(); err == nil {
			t.Fatalf("Validate(%#v) succeeded", input)
		}
	}
}

func TestCreateInputAcceptsEverySupportedKindAndDecision(t *testing.T) {
	kinds := []Kind{KindCommand, KindFileChange, KindNetwork, KindPermissions}
	decisions := []Decision{DecisionAcceptOnce, DecisionAcceptSession, DecisionDecline, DecisionCancelRun}
	for _, kind := range kinds {
		input := CreateInput{ExternalRequestID: "request", Kind: kind, Available: decisions}
		if err := input.Validate(); err != nil {
			t.Fatalf("kind %s: %v", kind, err)
		}
	}
}
