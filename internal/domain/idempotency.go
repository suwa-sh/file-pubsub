package domain

// PendingSubscriptions returns, from all configured subscriptions of a topic,
// the ones that still need delivery. Subscriptions with a delivered record are
// excluded to prevent double delivery (SR-003); subscriptions isolated to DLQ
// are excluded from automatic redelivery (SR-004, replay is the only way back).
// The order of all is preserved.
func PendingSubscriptions(states map[string]SubscriptionStatus, all []string) []string {
	pending := make([]string, 0, len(all))
	for _, name := range all {
		switch states[name] {
		case SubscriptionDelivered, SubscriptionDLQ:
			continue
		}
		pending = append(pending, name)
	}
	return pending
}
