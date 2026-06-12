// Package logging emits one JSON line per event (CTP-001) using log/slog.
// Fields follow schemas.log_line_json: logged_at / message_id / topic /
// subscription / event_type / error_detail. Subscription delivery events must
// carry message_id + topic + subscription so any failed delivery can be
// pinpointed.
package logging

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// Event is one structured log event (E-013).
type Event struct {
	MessageID    string
	Topic        string
	Subscription string
	EventType    string // collect / archive / fanout / retry / dlq / replay / startup / shutdown etc.
	ErrorDetail  string // cause + remedy, error events only
}

// Logger writes one JSON object per line to w.
type Logger struct {
	sl  *slog.Logger
	now func() time.Time
}

// New builds a logger writing to w (stdout or a log file).
func New(w io.Writer) *Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				a.Key = "logged_at"
			case slog.LevelKey, slog.MessageKey:
				return slog.Attr{} // event_type carries the meaning; no level / msg fields
			}
			return a
		},
	})
	return &Logger{sl: slog.New(handler), now: time.Now}
}

// Emit writes the event as one JSON line. Logging failures are swallowed so
// they never stop delivery processing (C-09).
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
