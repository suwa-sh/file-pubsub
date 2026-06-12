package domain

import (
	"reflect"
	"testing"
)

func TestPendingSubscriptions_配信状態の組み合わせごとに_未配信のsubscriptionだけが返ること(t *testing.T) {
	// Arrange
	tests := []struct {
		name   string
		states map[string]SubscriptionStatus
		all    []string
		want   []string
	}{
		{
			name:   "記録がまだ無い場合_全件が未配信になること",
			states: map[string]SubscriptionStatus{},
			all:    []string{"current", "next"},
			want:   []string{"current", "next"},
		},
		{
			name:   "deliveredとfailedが混在する場合_deliveredが除外されfailedが再配信対象になること",
			states: map[string]SubscriptionStatus{"current": SubscriptionDelivered, "next": SubscriptionFailed},
			all:    []string{"current", "next"},
			want:   []string{"next"},
		},
		{
			name:   "全件deliveredの場合_メッセージがスキップされること",
			states: map[string]SubscriptionStatus{"current": SubscriptionDelivered, "next": SubscriptionDelivered},
			all:    []string{"current", "next"},
			want:   []string{},
		},
		{
			name:   "dlqがある場合_自動再配信から除外されること",
			states: map[string]SubscriptionStatus{"current": SubscriptionDLQ, "next": SubscriptionFailed},
			all:    []string{"current", "next"},
			want:   []string{"next"},
		},
		{
			name:   "設定に後から追加されたsubscriptionがある場合_未配信として扱われること",
			states: map[string]SubscriptionStatus{"current": SubscriptionDelivered},
			all:    []string{"current", "next", "test"},
			want:   []string{"next", "test"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := PendingSubscriptions(tt.states, tt.all)

			// Assert
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PendingSubscriptions = %v, want %v", got, tt.want)
			}
		})
	}
}
