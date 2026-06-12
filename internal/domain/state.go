package domain

import "fmt"

// MessageStatus is the message delivery state recorded in the manifest
// (manifest_json.status). The manifest is the single source of truth (CTR-003).
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

// Valid reports whether s is a defined message status.
func (s MessageStatus) Valid() bool {
	switch s {
	case StatusCollected, StatusArchived, StatusDelivering, StatusDelivered,
		StatusFailed, StatusRetrying, StatusDLQ:
		return true
	}
	return false
}

// transitions encodes the message delivery state machine (LR-201):
// collected -> archived -> delivering -> delivered / failed,
// failed -> retrying, retrying -> delivering / failed / dlq.
var transitions = map[MessageStatus]map[MessageStatus]bool{
	StatusCollected:  {StatusArchived: true},
	StatusArchived:   {StatusDelivering: true},
	StatusDelivering: {StatusDelivered: true, StatusFailed: true},
	StatusFailed:     {StatusRetrying: true},
	StatusRetrying:   {StatusDelivering: true, StatusFailed: true, StatusDLQ: true},
	StatusDelivered:  {},
	StatusDLQ:        {},
}

// ValidateTransition returns an error when from -> to is not an allowed
// message delivery state transition.
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

// SubscriptionStatus is the per-subscription delivery state recorded in
// manifest_json.subscriptions[].status.
type SubscriptionStatus string

const (
	SubscriptionDelivered SubscriptionStatus = "delivered"
	SubscriptionFailed    SubscriptionStatus = "failed"
	SubscriptionDLQ       SubscriptionStatus = "dlq"
)

// Valid reports whether s is a defined subscription delivery status.
func (s SubscriptionStatus) Valid() bool {
	switch s {
	case SubscriptionDelivered, SubscriptionFailed, SubscriptionDLQ:
		return true
	}
	return false
}
