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

// writeConfig は有効な config.yaml (トピック "orders" 1 件 + サブスクリプション
// current / next) を専用の一時ディレクトリに書き、そのパスを返す。
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
	return cfgPath, base // data_dir の既定値は config.yaml のあるディレクトリ
}

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = run(args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestRun_不明なコマンドの場合_終了コード2になること(t *testing.T) {
	// Arrange & Act
	code, _, stderr := runCLI(t, "bogus")

	// Assert
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("stderr = %q", stderr)
	}

	// Act & Assert: 引数なしも終了コード 2
	if code, _, _ := runCLI(t); code != exitUsage {
		t.Fatal("no args must exit 2")
	}
}

func TestCmdConfigValidate_設定が正しい場合_OKと件数が出力されること(t *testing.T) {
	// Arrange
	cfgPath, _ := writeConfig(t)

	// Act
	code, stdout, _ := runCLI(t, "config", "validate", "--config", cfgPath)

	// Assert
	if code != exitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "OK: topics=1 subscriptions=2 sources=1") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestCmdConfigValidate_設定に複数の違反がある場合_全件報告され終了コード2になること(t *testing.T) {
	// Arrange: polling_interval と topics の 2 つの違反を同時に仕込む
	base := t.TempDir()
	cfgPath := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("archive_retention: 90\nretry_max_count: 5\nmetrics_port: 9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	code, _, stderr := runCLI(t, "config", "validate", "--config", cfgPath)

	// Assert
	if code != exitUsage {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "NG: polling_interval") || !strings.Contains(stderr, "NG: topics") {
		t.Fatalf("all violations must be reported, stderr = %q", stderr)
	}
}

func TestCmdConfigValidate_configフラグが無い場合_終了コード2になること(t *testing.T) {
	// Arrange & Act & Assert
	if code, _, _ := runCLI(t, "config", "validate"); code != exitUsage {
		t.Fatal("missing --config must exit 2")
	}
	if code, _, _ := runCLI(t, "config"); code != exitUsage {
		t.Fatal("bare config must exit 2")
	}
}

// seedManifest は current=delivered / next=failed のマニフェストを 1 件書き込む。
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

func TestCmdStatus_マニフェストがある場合_テーブルとサマリが出力されること(t *testing.T) {
	// Arrange
	cfgPath, dataDir := writeConfig(t)
	id := seedManifest(t, dataDir)

	// Act
	code, stdout, _ := runCLI(t, "status", "--config", cfgPath)

	// Assert
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
	// grep しやすい形式: message_id + failed + リトライ回数が 1 行に載る。
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

func TestCmdStatus_statusフィルタを指定した場合_該当行のみ出力されること(t *testing.T) {
	// Arrange
	cfgPath, dataDir := writeConfig(t)
	seedManifest(t, dataDir)

	// Act
	code, stdout, _ := runCLI(t, "status", "--config", cfgPath, "--topic", "orders", "--status", "failed")

	// Assert
	if code != exitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "failed") || strings.Contains(stdout, "delivered  ") {
		t.Fatalf("only failed rows expected, got %q", stdout)
	}
}

func TestCmdStatus_引数が不正な場合_終了コード2になること(t *testing.T) {
	// Arrange
	cfgPath, _ := writeConfig(t)
	cases := [][]string{
		{"status", "--config", cfgPath, "--status", "pending"},
		{"status", "--config", cfgPath, "--topic", "unknown-topic"},
		{"status", "--config", cfgPath, "--subscription", "unknown-sub"},
		{"status"},
	}
	for _, args := range cases {
		// Act & Assert
		if code, _, stderr := runCLI(t, args...); code != exitUsage {
			t.Fatalf("%v: exit = %d (stderr %q), want 2", args, code, stderr)
		}
	}
}

func TestCmdStatus_dlqを指定した場合_DLQテーブルとサマリが出力されること(t *testing.T) {
	// Arrange: DLQ 隔離済みメッセージを 1 件作る
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

	// Act
	code, stdout, _ := runCLI(t, "status", "--config", cfgPath, "--status", "dlq")

	// Assert
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

func TestCmdReplay_引数が不正な場合_終了コード2になること(t *testing.T) {
	// Arrange
	cfgPath, _ := writeConfig(t)
	cases := [][]string{
		{"replay", "--config", cfgPath, "--topic", "orders", "--subscription", "next"},                                                                    // message-id も期間も無い
		{"replay", "--config", cfgPath, "--topic", "orders", "--message-id", "x", "--from", "2026-05-01", "--to", "2026-05-31", "--subscription", "next"}, // 両方指定
		{"replay", "--config", cfgPath, "--topic", "orders", "--message-id", "x"},                                                                         // subscription 無し
		{"replay", "--config", cfgPath, "--topic", "orders", "--message-id", "x", "--subscription", "nope"},                                               // 未定義 subscription
		{"replay", "--config", cfgPath, "--topic", "nope", "--message-id", "x", "--subscription", "next"},                                                 // 未定義 topic
		{"replay", "--config", cfgPath, "--topic", "orders", "--from", "05/01", "--to", "2026-05-31", "--subscription", "next"},                           // 不正な日付
	}
	for _, args := range cases {
		// Act & Assert
		if code, _, _ := runCLI(t, args...); code != exitUsage {
			t.Fatalf("%v: exit code must be 2", args)
		}
	}
}

func TestCmdReplay_messageIDを指定した場合_指定サブスクリプションにのみ配置されサマリが出力されること(t *testing.T) {
	// Arrange
	cfgPath, dataDir := writeConfig(t)
	id := seedManifest(t, dataDir)
	archivePath := store.NewArchiveStore(dataDir).ArchivePath("orders", id)
	if err := store.WriteFileAtomic(archivePath, strings.NewReader("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	code, stdout, stderr := runCLI(t, "replay", "--config", cfgPath, "--topic", "orders", "--message-id", id, "--subscription", "next")

	// Assert
	if code != exitOK {
		t.Fatalf("exit = %d (stderr %q), want 0", code, stderr)
	}
	for _, want := range []string{"topic: orders", "message_id: " + id, "subscription: next", "replayed: 1", "status command"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("summary missing %q, stdout = %q", want, stdout)
		}
	}
	// リプレイされたファイルは next にのみ存在し、status にリプレイが表示される。
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

// TestCmdReplay_serveがロックを保持している場合_終了コード3で拒否されること は
// single-writer のルールを守るテスト: serve が data-dir ロックを保持している間、
// replay は実行を拒否し (終了コード 3)、ロックが解放されたら成功しなければならない。
func TestCmdReplay_serveがロックを保持している場合_終了コード3で拒否されること(t *testing.T) {
	// Arrange: 生きたロック保持者 (このテストプロセス) で serve 実行中を再現する
	cfgPath, dataDir := writeConfig(t)
	args := []string{"replay", "--config", cfgPath, "--topic", "orders",
		"--from", "2026-04-01", "--to", "2026-04-30", "--subscription", "next"}
	lock := store.NewLockManager(dataDir)
	if err := lock.Acquire(os.Getpid(), time.Now()); err != nil {
		t.Fatal(err)
	}

	// Act
	code, _, stderr := runCLI(t, args...)

	// Assert
	if code != exitDuplicate {
		t.Fatalf("exit = %d (stderr %q), want %d while the lock is held", code, stderr, exitDuplicate)
	}
	if !strings.Contains(stderr, "serve is running") {
		t.Fatalf("stderr must explain that serve is running, got %q", stderr)
	}

	// Act: ロック解放後に同じ replay を実行
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLI(t, args...)

	// Assert: 成功し、replay 自身が取得したロックも完了時に解放される
	if code != exitOK {
		t.Fatalf("exit = %d (stderr %q), want 0 without a lock holder", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "lock")); !os.IsNotExist(err) {
		t.Fatalf("replay must release its lock on completion, stat err = %v", err)
	}
}

func TestCmdReplay_対象が0件の場合_正常終了すること(t *testing.T) {
	// Arrange
	cfgPath, _ := writeConfig(t)

	// Act
	code, stdout, _ := runCLI(t, "replay", "--config", cfgPath, "--topic", "orders",
		"--from", "2026-04-01", "--to", "2026-04-30", "--subscription", "next")

	// Assert
	if code != exitOK {
		t.Fatalf("exit = %d, want 0 (zero targets is a normal result)", code)
	}
	if !strings.Contains(stdout, "replayed: 0") {
		t.Fatalf("stdout = %q", stdout)
	}
}
