package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suwa-sh/file-pubsub/internal/domain"
	"github.com/suwa-sh/file-pubsub/internal/gateway/store"
)

// writeConfig writes a valid config.yaml (one topic "orders" with
// subscriptions current / next) into its own temp dir and returns its path.
func writeConfig(t *testing.T) (cfgPath, dataDir string) {
	t.Helper()
	base := t.TempDir()
	srcDir := filepath.Join(base, "src")
	cur := filepath.Join(base, "subs", "current")
	next := filepath.Join(base, "subs", "next")
	for _, d := range []string{srcDir, cur, next} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath = filepath.Join(base, "config.yaml")
	yaml := fmt.Sprintf(`polling_interval: 60
archive_retention: 90
retry_max_count: 5
metrics_port: 9090
topics:
  - name: orders
    source:
      type: local
      directory: %s
      stability_check:
        interval: 10
    subscriptions:
      - name: current
        directory: %s
      - name: next
        directory: %s
`, srcDir, cur, next)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath, base // data_dir defaults to the config.yaml directory
}

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestUnknownCommandExitsUsage(t *testing.T) {
	code, _, stderr := runCLI(t, "bogus")
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("stderr = %q", stderr)
	}
	if code, _, _ := runCLI(t); code != exitUsage {
		t.Fatal("no args must exit 2")
	}
}

func TestConfigValidateOK(t *testing.T) {
	cfgPath, _ := writeConfig(t)
	code, stdout, _ := runCLI(t, "config", "validate", "--config", cfgPath)
	if code != exitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "OK: topics=1 subscriptions=2 sources=1") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestConfigValidateNGReportsAllErrors(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, "config.yaml")
	// Two violations at once: missing polling_interval and topics.
	if err := os.WriteFile(cfgPath, []byte("archive_retention: 90\nretry_max_count: 5\nmetrics_port: 9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI(t, "config", "validate", "--config", cfgPath)
	if code != exitUsage {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "NG: polling_interval") || !strings.Contains(stderr, "NG: topics") {
		t.Fatalf("all violations must be reported, stderr = %q", stderr)
	}
}

func TestConfigValidateMissingFlag(t *testing.T) {
	if code, _, _ := runCLI(t, "config", "validate"); code != exitUsage {
		t.Fatal("missing --config must exit 2")
	}
	if code, _, _ := runCLI(t, "config"); code != exitUsage {
		t.Fatal("bare config must exit 2")
	}
}

// seedManifest writes one manifest with current=delivered, next=failed.
func seedManifest(t *testing.T, dataDir string) string {
	t.Helper()
	collected := time.Date(2026, 6, 1, 9, 15, 0, 0, time.UTC)
	msg := domain.NewMessage(collected, "orders", "orders_1.csv")
	m := store.NewManifest(msg)
	deliveredAt := collected.Add(8 * time.Second)
	m.Status = domain.StatusFailed
	m.RetryCount = 2
	m.SetSubscriptionState("current", domain.SubscriptionDelivered, &deliveredAt, "")
	m.SetSubscriptionState("next", domain.SubscriptionFailed, nil, "permission denied (write)")
	if err := store.NewManifestStore(dataDir).Put(m); err != nil {
		t.Fatal(err)
	}
	return m.MessageID
}

func TestStatusTableAndSummary(t *testing.T) {
	cfgPath, dataDir := writeConfig(t)
	id := seedManifest(t, dataDir)

	code, stdout, _ := runCLI(t, "status", "--config", cfgPath)
	if code != exitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	header := strings.Fields(lines[0])
	want := []string{"MESSAGE_ID", "TOPIC", "SUBSCRIPTION", "STATUS", "RETRY", "DELIVERED_AT", "REPLAY"}
	if strings.Join(header, " ") != strings.Join(want, " ") {
		t.Fatalf("header = %v, want %v", header, want)
	}
	if !strings.Contains(stdout, id) {
		t.Fatalf("stdout must contain the message_id, got %q", stdout)
	}
	// grep-style: one line carries message_id + failed + retry count.
	var failedLine string
	for _, l := range lines {
		if strings.Contains(l, id) && strings.Contains(l, "failed") {
			failedLine = l
		}
	}
	fields := strings.Fields(failedLine)
	if len(fields) != 7 || fields[3] != "failed" || fields[4] != "2" || fields[5] != "-" || fields[6] != "-" {
		t.Fatalf("failed row = %v", fields)
	}
	if !strings.Contains(stdout, "orders/current: delivered=1 failed=0 dlq=0") ||
		!strings.Contains(stdout, "orders/next: delivered=0 failed=1 dlq=0") {
		t.Fatalf("summary missing, stdout = %q", stdout)
	}
}

func TestStatusFilterByStatus(t *testing.T) {
	cfgPath, dataDir := writeConfig(t)
	seedManifest(t, dataDir)

	code, stdout, _ := runCLI(t, "status", "--config", cfgPath, "--topic", "orders", "--status", "failed")
	if code != exitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "failed") || strings.Contains(stdout, "delivered  ") {
		t.Fatalf("only failed rows expected, got %q", stdout)
	}
}

func TestStatusArgumentValidation(t *testing.T) {
	cfgPath, _ := writeConfig(t)
	cases := [][]string{
		{"status", "--config", cfgPath, "--status", "pending"},
		{"status", "--config", cfgPath, "--topic", "unknown-topic"},
		{"status", "--config", cfgPath, "--subscription", "unknown-sub"},
		{"status"},
	}
	for _, args := range cases {
		if code, _, stderr := runCLI(t, args...); code != exitUsage {
			t.Fatalf("%v: exit = %d (stderr %q), want 2", args, code, stderr)
		}
	}
}

func TestStatusDLQView(t *testing.T) {
	cfgPath, dataDir := writeConfig(t)
	archive := filepath.Join(dataDir, "archive", "orders", "m1")
	if err := store.WriteFileAtomic(archive, strings.NewReader("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := store.DLQMeta{
		MessageID:       "20260611T220500_orders_inv.csv",
		Topic:           "orders",
		IsolationReason: "permission denied (write)",
		FailureCount:    5,
		IsolatedAt:      time.Date(2026, 6, 11, 22, 31, 10, 0, time.UTC),
	}
	if err := store.NewDLQStore(dataDir).Isolate(archive, meta); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "status", "--config", cfgPath, "--status", "dlq")
	if code != exitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	header := strings.Fields(lines[0])
	want := []string{"MESSAGE_ID", "TOPIC", "ISOLATION_REASON", "FAILURES", "ISOLATED_AT"}
	if strings.Join(header, " ") != strings.Join(want, " ") {
		t.Fatalf("header = %v, want %v", header, want)
	}
	if !strings.Contains(stdout, meta.MessageID) || !strings.Contains(stdout, "permission denied (write)") {
		t.Fatalf("dlq row missing, stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "orders: dlq=1") {
		t.Fatalf("dlq summary missing, stdout = %q", stdout)
	}
}

func TestReplayArgumentValidation(t *testing.T) {
	cfgPath, _ := writeConfig(t)
	cases := [][]string{
		{"replay", "--config", cfgPath, "--topic", "orders", "--subscription", "next"},                                                                    // neither
		{"replay", "--config", cfgPath, "--topic", "orders", "--message-id", "x", "--from", "2026-05-01", "--to", "2026-05-31", "--subscription", "next"}, // both
		{"replay", "--config", cfgPath, "--topic", "orders", "--message-id", "x"},                                                                         // no subscription
		{"replay", "--config", cfgPath, "--topic", "orders", "--message-id", "x", "--subscription", "nope"},                                               // unknown subscription
		{"replay", "--config", cfgPath, "--topic", "nope", "--message-id", "x", "--subscription", "next"},                                                 // unknown topic
		{"replay", "--config", cfgPath, "--topic", "orders", "--from", "05/01", "--to", "2026-05-31", "--subscription", "next"},                           // bad date
	}
	for _, args := range cases {
		if code, _, _ := runCLI(t, args...); code != exitUsage {
			t.Fatalf("%v: exit code must be 2", args)
		}
	}
}

func TestReplayPlacesAndSummarizes(t *testing.T) {
	cfgPath, dataDir := writeConfig(t)
	id := seedManifest(t, dataDir)
	archivePath := store.NewArchiveStore(dataDir).ArchivePath("orders", id)
	if err := store.WriteFileAtomic(archivePath, strings.NewReader("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCLI(t, "replay", "--config", cfgPath, "--topic", "orders", "--message-id", id, "--subscription", "next")
	if code != exitOK {
		t.Fatalf("exit = %d (stderr %q), want 0", code, stderr)
	}
	for _, want := range []string{"topic: orders", "message_id: " + id, "subscription: next", "replayed: 1", "status command"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("summary missing %q, stdout = %q", want, stdout)
		}
	}

	// The replayed file is in next only, and status now shows the replay.
	nextDir := filepath.Join(dataDir, "subs", "next")
	if _, err := os.Stat(filepath.Join(nextDir, "orders_1.csv")); err != nil {
		t.Fatalf("replayed file missing: %v", err)
	}
	curDir := filepath.Join(dataDir, "subs", "current")
	if _, err := os.Stat(filepath.Join(curDir, "orders_1.csv")); err == nil {
		t.Fatal("current must not receive the replayed file")
	}
	_, statusOut, _ := runCLI(t, "status", "--config", cfgPath, "--subscription", "next")
	if !strings.Contains(statusOut, "replay") {
		t.Fatalf("status must mark the replayed delivery, got %q", statusOut)
	}
}

func TestReplayZeroTargetsIsSuccess(t *testing.T) {
	cfgPath, _ := writeConfig(t)
	code, stdout, _ := runCLI(t, "replay", "--config", cfgPath, "--topic", "orders",
		"--from", "2026-04-01", "--to", "2026-04-30", "--subscription", "next")
	if code != exitOK {
		t.Fatalf("exit = %d, want 0 (zero targets is a normal result)", code)
	}
	if !strings.Contains(stdout, "replayed: 0") {
		t.Fatalf("stdout = %q", stdout)
	}
}
