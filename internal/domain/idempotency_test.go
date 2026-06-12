package domain

import (
	"reflect"
	"testing"
)

func TestPendingSubscriptions(t *testing.T) {
	tests := []struct {
		name   string
		states map[string]SubscriptionStatus
		all    []string
		want   []string
	}{
		{
			name:   "no records yet: all pending",
			states: map[string]SubscriptionStatus{},
			all:    []string{"current", "next"},
			want:   []string{"current", "next"},
		},
		{
			name:   "delivered excluded, failed retried",
			states: map[string]SubscriptionStatus{"current": SubscriptionDelivered, "next": SubscriptionFailed},
			all:    []string{"current", "next"},
			want:   []string{"next"},
		},
		{
			name:   "all delivered: message is skipped",
			states: map[string]SubscriptionStatus{"current": SubscriptionDelivered, "next": SubscriptionDelivered},
			all:    []string{"current", "next"},
			want:   []string{},
		},
		{
			name:   "dlq excluded from automatic redelivery",
			states: map[string]SubscriptionStatus{"current": SubscriptionDLQ, "next": SubscriptionFailed},
			all:    []string{"current", "next"},
			want:   []string{"next"},
		},
		{
			name:   "subscription added to config later is pending",
			states: map[string]SubscriptionStatus{"current": SubscriptionDelivered},
			all:    []string{"current", "next", "test"},
			want:   []string{"next", "test"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PendingSubscriptions(tt.states, tt.all)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PendingSubscriptions = %v, want %v", got, tt.want)
			}
		})
	}
}
