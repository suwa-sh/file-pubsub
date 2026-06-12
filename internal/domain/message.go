// Package domain はファイル・ネットワーク I/O を持たない純粋ロジックを置く (LP-201)。
package domain

import "time"

// Message はパイプラインを流れる収集済みファイル 1 件を表す (E-006)。
type Message struct {
	MessageID        string
	Topic            string
	OriginalFileName string
	CollectedAt      time.Time
}

// NewMessageID は収集時刻 + topic + 元ファイル名から message_id を導出する
// (SR-002, LR-202)。同名ファイルでも収集時刻が異なれば別 ID が採番されるため、
// 履歴が上書きされることはない。
// 形式: YYYYMMDDTHHMMSS_{topic}_{originalFileName}。
func NewMessageID(collectedAt time.Time, topic, originalFileName string) string {
	return collectedAt.Format("20060102T150405") + "_" + topic + "_" + originalFileName
}

// NewMessage は ID を採番済みの Message を生成する。
func NewMessage(collectedAt time.Time, topic, originalFileName string) Message {
	return Message{
		MessageID:        NewMessageID(collectedAt, topic, originalFileName),
		Topic:            topic,
		OriginalFileName: originalFileName,
		CollectedAt:      collectedAt,
	}
}
