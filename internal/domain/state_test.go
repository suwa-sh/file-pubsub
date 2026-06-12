package domain

import "testing"

func TestValidateTransition_Allowed(t *testing.T) {
	allowed := [][2]MessageStatus{
		{StatusCollected, StatusArchived},
		{StatusArchived, StatusDelivering},
		{StatusDelivering, StatusDelivered},
		{StatusDelivering, StatusFailed},
		{StatusFailed, StatusRetrying},
		{StatusRetrying, StatusDelivering},
		{StatusRetrying, StatusFailed},
		{StatusRetrying, StatusDLQ},
	}
	for _, tr := range allowed {
		if err := ValidateTransition(tr[0], tr[1]); err != nil {
			t.Errorf("ValidateTransition(%s, %s) = %v, want nil", tr[0], tr[1], err)
		}
	}
}

func TestValidateTransition_Rejected(t *testing.T) {
	rejected := [][2]MessageStatus{
		{StatusCollected, StatusDelivering}, // archive save is mandatory before fan-out (SP-001)
		{StatusCollected, StatusDelivered},
		{StatusArchived, StatusDelivered},
		{StatusDelivered, StatusDelivering},
		{StatusDLQ, StatusDelivering}, // dlq leaves the automatic pipeline
		{StatusFailed, StatusDLQ},     // isolation goes through retrying
	}
	for _, tr := range rejected {
		if err := ValidateTransition(tr[0], tr[1]); err == nil {
			t.Errorf("ValidateTransition(%s, %s) = nil, want error", tr[0], tr[1])
		}
	}
}

func TestValidateTransition_InvalidStatus(t *testing.T) {
	if err := ValidateTransition("bogus", StatusArchived); err == nil {
		t.Error("invalid from status must be rejected")
	}
	if err := ValidateTransition(StatusCollected, "bogus"); err == nil {
		t.Error("invalid to status must be rejected")
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range []MessageStatus{StatusCollected, StatusArchived, StatusDelivering, StatusDelivered, StatusFailed, StatusRetrying, StatusDLQ} {
		if !s.Valid() {
			t.Errorf("%s must be valid", s)
		}
	}
	if MessageStatus("unknown").Valid() {
		t.Error("unknown status must be invalid")
	}
}

func TestSubscriptionStatusValid(t *testing.T) {
	for _, s := range []SubscriptionStatus{SubscriptionDelivered, SubscriptionFailed, SubscriptionDLQ} {
		if !s.Valid() {
			t.Errorf("%s must be valid", s)
		}
	}
	if SubscriptionStatus("unknown").Valid() {
		t.Error("unknown subscription status must be invalid")
	}
}
