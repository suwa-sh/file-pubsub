package domain

import "fmt"

// MessageStatus は manifest に記録されるメッセージの配信状態
// (manifest_json.status)。manifest が唯一の正本である (CTR-003)。
type MessageStatus string

const (
	StatusCollected  MessageStatus = "collected"
	StatusArchived   MessageStatus = "archived"
	StatusDelivering MessageStatus = "delivering"
	StatusDelivered  MessageStatus = "delivered"
	StatusFailed     MessageStatus = "failed"
	StatusRetrying   MessageStatus = "retrying"
	StatusDLQ        MessageStatus = "dlq"
)

// Valid は s が定義済みのメッセージ状態かどうかを返す。
func (s MessageStatus) Valid() bool {
	switch s {
	case StatusCollected, StatusArchived, StatusDelivering, StatusDelivered,
		StatusFailed, StatusRetrying, StatusDLQ:
		return true
	}
	return false
}

// transitions はメッセージ配信の状態遷移マシンを表す (LR-201):
// collected -> archived -> delivering -> delivered / failed,
// failed -> retrying, retrying -> delivering / failed / dlq。
var transitions = map[MessageStatus]map[MessageStatus]bool{
	StatusCollected:  {StatusArchived: true},
	StatusArchived:   {StatusDelivering: true},
	StatusDelivering: {StatusDelivered: true, StatusFailed: true},
	StatusFailed:     {StatusRetrying: true},
	StatusRetrying:   {StatusDelivering: true, StatusFailed: true, StatusDLQ: true},
	StatusDelivered:  {},
	StatusDLQ:        {},
}

// ValidateTransition は from -> to がメッセージ配信状態の許可された遷移で
// ない場合にエラーを返す。
func ValidateTransition(from, to MessageStatus) error {
	if !from.Valid() {
		return fmt.Errorf("invalid message status %q", from)
	}
	if !to.Valid() {
		return fmt.Errorf("invalid message status %q", to)
	}
	if !transitions[from][to] {
		return fmt.Errorf("invalid transition from %q to %q", from, to)
	}
	return nil
}

// SubscriptionStatus は manifest_json.subscriptions[].status に記録される
// subscription 単位の配信状態。
type SubscriptionStatus string

const (
	SubscriptionDelivered SubscriptionStatus = "delivered"
	SubscriptionFailed    SubscriptionStatus = "failed"
	SubscriptionDLQ       SubscriptionStatus = "dlq"
)

// Valid は s が定義済みの subscription 配信状態かどうかを返す。
func (s SubscriptionStatus) Valid() bool {
	switch s {
	case SubscriptionDelivered, SubscriptionFailed, SubscriptionDLQ:
		return true
	}
	return false
}
