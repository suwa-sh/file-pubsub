package domain

// PendingSubscriptions は、topic に設定された全 subscription のうち、まだ配信が必要な
// ものを返す。delivered 記録がある subscription は二重配信防止のため除外し (SR-003)、
// DLQ に隔離された subscription は自動再配信の対象外とする (SR-004、復帰は replay のみ)。
// all の順序は保持する。
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
