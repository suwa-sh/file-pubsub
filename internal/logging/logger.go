// Package logging は log/slog を使い、イベントごとに 1 行の JSON を出力する
// (CTP-001)。フィールドは schemas.log_line_json に従う: logged_at /
// message_id / topic / subscription / event_type / error_detail。
// サブスクリプション配信イベントは、失敗した配信を特定できるよう
// message_id + topic + subscription を必ず持つ。
package logging

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// Event は構造化ログイベント 1 件 (E-013)。
type Event struct {
	MessageID    string
	Topic        string
	Subscription string
	EventType    string // collect / archive / fanout / retry / dlq / replay / startup / shutdown など
	ErrorDetail  string // 原因 + 対処。エラーイベントのみ
}

// Logger は w に 1 行 1 JSON オブジェクトを書き込む。
type Logger struct {
	sl  *slog.Logger
	now func() time.Time
}

// New は w (stdout またはログファイル) へ書き込むロガーを生成する。
func New(w io.Writer) *Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				a.Key = "logged_at"
			case slog.LevelKey, slog.MessageKey:
				return slog.Attr{} // 意味は event_type が担う。level / msg フィールドは出さない
			}
			return a
		},
	})
	return &Logger{sl: slog.New(handler), now: time.Now}
}

// Emit はイベントを 1 行の JSON として書き込む。ログ出力の失敗は握りつぶし、
// 配信処理を決して止めない (C-09)。
func (l *Logger) Emit(e Event) {
	attrs := make([]slog.Attr, 0, 5)
	if e.MessageID != "" {
		attrs = append(attrs, slog.String("message_id", e.MessageID))
	}
	if e.Topic != "" {
		attrs = append(attrs, slog.String("topic", e.Topic))
	}
	if e.Subscription != "" {
		attrs = append(attrs, slog.String("subscription", e.Subscription))
	}
	attrs = append(attrs, slog.String("event_type", e.EventType))
	if e.ErrorDetail != "" {
		attrs = append(attrs, slog.String("error_detail", e.ErrorDetail))
	}
	_ = l.sl.Handler().Handle(context.Background(), newRecord(l.now(), attrs))
}

func newRecord(at time.Time, attrs []slog.Attr) slog.Record {
	rec := slog.NewRecord(at, slog.LevelInfo, "", 0)
	rec.AddAttrs(attrs...)
	return rec
}
