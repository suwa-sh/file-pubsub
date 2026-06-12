package domain

// ShouldIsolate reports whether a failed delivery has exhausted its retries
// and must be isolated to DLQ (SR-004): retryCount >= retryMaxCount.
// Within the limit the delivery is retried.
func ShouldIsolate(retryCount, retryMaxCount int) bool {
	return retryCount >= retryMaxCount
}
