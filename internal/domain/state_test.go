package domain

import "testing"

func TestValidateTransition_許可された遷移の場合_エラーにならないこと(t *testing.T) {
	// Arrange
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
		// Act
		err := ValidateTransition(tr[0], tr[1])

		// Assert
		if err != nil {
			t.Errorf("ValidateTransition(%s, %s) = %v, want nil", tr[0], tr[1], err)
		}
	}
}

func TestValidateTransition_許可されない遷移の場合_エラーになること(t *testing.T) {
	// Arrange
	rejected := [][2]MessageStatus{
		{StatusCollected, StatusDelivering}, // fan-out の前に archive 保存が必須 (SP-001)
		{StatusCollected, StatusDelivered},
		{StatusArchived, StatusDelivered},
		{StatusDelivered, StatusDelivering},
		{StatusDLQ, StatusDelivering}, // dlq は自動パイプラインから離脱する
		{StatusFailed, StatusDLQ},     // 隔離は retrying を経由する
	}
	for _, tr := range rejected {
		// Act
		err := ValidateTransition(tr[0], tr[1])

		// Assert
		if err == nil {
			t.Errorf("ValidateTransition(%s, %s) = nil, want error", tr[0], tr[1])
		}
	}
}

func TestValidateTransition_未定義の状態を渡した場合_エラーになること(t *testing.T) {
	// Arrange & Act & Assert
	if err := ValidateTransition("bogus", StatusArchived); err == nil {
		t.Error("invalid from status must be rejected")
	}
	if err := ValidateTransition(StatusCollected, "bogus"); err == nil {
		t.Error("invalid to status must be rejected")
	}
}

func TestMessageStatusValid_定義済みと未定義の状態を与えた場合_定義済みだけが有効と判定されること(t *testing.T) {
	// Arrange & Act & Assert
	for _, s := range []MessageStatus{StatusCollected, StatusArchived, StatusDelivering, StatusDelivered, StatusFailed, StatusRetrying, StatusDLQ} {
		if !s.Valid() {
			t.Errorf("%s must be valid", s)
		}
	}
	if MessageStatus("unknown").Valid() {
		t.Error("unknown status must be invalid")
	}
}

func TestSubscriptionStatusValid_定義済みと未定義の状態を与えた場合_定義済みだけが有効と判定されること(t *testing.T) {
	// Arrange & Act & Assert
	for _, s := range []SubscriptionStatus{SubscriptionDelivered, SubscriptionFailed, SubscriptionDLQ} {
		if !s.Valid() {
			t.Errorf("%s must be valid", s)
		}
	}
	if SubscriptionStatus("unknown").Valid() {
		t.Error("unknown subscription status must be invalid")
	}
}
