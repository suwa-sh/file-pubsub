package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/logging"
	"github.com/suwa-sh/file-pubsub/internal/usecase"
)

func cmdReplay(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("replay", stderr)
	cfgPath := fs.String("config", "", "path of the config YAML")
	topic := fs.String("topic", "", "topic of the messages to replay")
	messageID := fs.String("message-id", "", "replay one message by message_id")
	from := fs.String("from", "", "period start (YYYY-MM-DD, by collected_at)")
	to := fs.String("to", "", "period end (YYYY-MM-DD, inclusive)")
	subscription := fs.String("subscription", "", "destination subscription (files are placed only here)")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitUsage
	}

	params := usecase.ReplayParams{Topic: *topic, MessageID: *messageID, Subscription: *subscription}
	var err error
	if params.From, err = parseDate(*from); err != nil {
		fmt.Fprintf(stderr, "--from %q is not a date: use the YYYY-MM-DD form\n", *from)
		return exitUsage
	}
	if params.To, err = parseDate(*to); err != nil {
		fmt.Fprintf(stderr, "--to %q is not a date: use the YYYY-MM-DD form\n", *to)
		return exitUsage
	}

	pipe := usecase.NewPipeline(cfg, logging.New(stderr), nil)
	count, err := pipe.Replay(context.Background(), params)
	if err != nil {
		fmt.Fprintln(stderr, err)
		var usage usecase.UsageError
		if errors.As(err, &usage) {
			return exitUsage
		}
		return exitRuntime
	}

	target := "message_id: " + params.MessageID
	if params.MessageID == "" {
		target = fmt.Sprintf("period: %s..%s", params.From.Format("2006-01-02"), params.To.Format("2006-01-02"))
	}
	fmt.Fprintf(stdout, "replay completed\ntopic: %s\n%s\nsubscription: %s\nreplayed: %d\n", params.Topic, target, params.Subscription, count)
	fmt.Fprintln(stdout, "the replay history is recorded in the manifest; check it with the status command")
	return exitOK
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", s)
}
