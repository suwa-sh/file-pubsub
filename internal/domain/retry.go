package domain

// ShouldIsolate は配信失敗がリトライ上限に達し、DLQ への隔離が必要かどうかを返す
// (SR-004): retryCount >= retryMaxCount。上限内であれば配信はリトライされる。
func ShouldIsolate(retryCount, retryMaxCount int) bool {
	return retryCount >= retryMaxCount
}
