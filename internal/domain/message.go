// Package domain holds pure logic with no file or network I/O (LP-201).
package domain

import "time"

// Message is one collected file flowing through the pipeline (E-006).
type Message struct {
	MessageID        string
	Topic            string
	OriginalFileName string
	CollectedAt      time.Time
}

// NewMessageID derives a message_id from collection time + topic + original
// file name (SR-002, LR-202). A same-name re-export at a different time gets a
// distinct ID, so history is never overwritten.
// Format: YYYYMMDDTHHMMSS_{topic}_{originalFileName}.
func NewMessageID(collectedAt time.Time, topic, originalFileName string) string {
	return collectedAt.Format("20060102T150405") + "_" + topic + "_" + originalFileName
}

// NewMessage builds a Message with its ID assigned.
func NewMessage(collectedAt time.Time, topic, originalFileName string) Message {
	return Message{
		MessageID:        NewMessageID(collectedAt, topic, originalFileName),
		Topic:            topic,
		OriginalFileName: originalFileName,
		CollectedAt:      collectedAt,
	}
}
