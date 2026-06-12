package main

import (
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/config"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
	"github.com/suwa-sh/file-pubsub/internal/usecase"
)

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func cmdStatus(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("status", stderr)
	cfgPath := fs.String("config", "", "path of the config YAML")
	topic := fs.String("topic", "", "filter by topic name")
	subscription := fs.String("subscription", "", "filter by subscription name")
	status := fs.String("status", "", "filter by delivery state (delivered / failed / dlq)")
	if fs.Parse(args) != nil {
		return exitUsage
	}
	cfg, ok := loadConfig(*cfgPath, stderr)
	if !ok {
		return exitUsage
	}
	if msg := validateStatusArgs(cfg, *topic, *subscription, *status); msg != "" {
		fmt.Fprintln(stderr, msg)
		return exitUsage
	}

	pipe := usecase.NewPipeline(cfg, nil, nil)
	if *status == "dlq" && *subscription == "" {
		metas, err := pipe.DLQList(*topic)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitRuntime
		}
		renderDLQTable(stdout, metas)
		return exitOK
	}

	rows, err := pipe.StatusRows(usecase.StatusFilter{Topic: *topic, Subscription: *subscription, Status: *status})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitRuntime
	}
	renderStatusTable(stdout, rows)
	return exitOK
}

func validateStatusArgs(cfg *config.Config, topic, subscription, status string) string {
	switch status {
	case "", "delivered", "failed", "dlq":
	default:
		return fmt.Sprintf("status %q is not a manifest state. use one of delivered / failed / dlq", status)
	}
	if topic != "" && findConfigTopic(cfg, topic) == nil {
		return fmt.Sprintf("topic %q is not defined in the config. check the topic name in the config file", topic)
	}
	if subscription != "" {
		found := false
		for i := range cfg.Topics {
			t := &cfg.Topics[i]
			if topic != "" && t.Name != topic {
				continue
			}
			for _, s := range t.Subscriptions {
				if s.Name == subscription {
					found = true
				}
			}
		}
		if !found {
			return fmt.Sprintf("subscription %q is not defined in the config. check the subscription name in the config file", subscription)
		}
	}
	return ""
}

func findConfigTopic(cfg *config.Config, name string) *config.Topic {
	for i := range cfg.Topics {
		if cfg.Topics[i].Name == name {
			return &cfg.Topics[i]
		}
	}
	return nil
}

func formatTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func renderStatusTable(w io.Writer, rows []usecase.StatusRow) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MESSAGE_ID\tTOPIC\tSUBSCRIPTION\tSTATUS\tRETRY\tDELIVERED_AT\tREPLAY")
	for _, r := range rows {
		replay := "-"
		if r.Replay {
			replay = "replay"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			r.MessageID, r.Topic, r.Subscription, r.Status, r.Retry, formatTime(r.DeliveredAt), replay)
	}
	tw.Flush()

	fmt.Fprintln(w)
	for _, s := range usecase.SummarizeStatus(rows) {
		fmt.Fprintf(w, "%s/%s: delivered=%d failed=%d dlq=%d\n", s.Topic, s.Subscription, s.Delivered, s.Failed, s.DLQ)
	}
}

func renderDLQTable(w io.Writer, metas []store.DLQMeta) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MESSAGE_ID\tTOPIC\tISOLATION_REASON\tFAILURES\tISOLATED_AT")
	counts := map[string]int{}
	var topics []string
	for _, m := range metas {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
			m.MessageID, m.Topic, m.IsolationReason, m.FailureCount, m.IsolatedAt.Format(time.RFC3339))
		if counts[m.Topic] == 0 {
			topics = append(topics, m.Topic)
		}
		counts[m.Topic]++
	}
	tw.Flush()

	fmt.Fprintln(w)
	for _, t := range topics {
		fmt.Fprintf(w, "%s: dlq=%d\n", t, counts[t])
	}
}
