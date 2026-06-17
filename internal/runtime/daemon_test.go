package runtime

import (
	"testing"

	"github.com/suwa-sh/file-pubsub/internal/config"
)

func daemonWith(topics ...config.Topic) *Daemon {
	return &Daemon{Cfg: &config.Config{PollingInterval: 60, Topics: topics}}
}

func inboxTopic(name, dir string, fallback int) config.Topic {
	return config.Topic{
		Name: name,
		Source: config.Source{
			Type:                 config.SourceTypeInbox,
			Directory:            dir,
			FallbackPollInterval: fallback,
		},
	}
}

func TestInboxDirs_inboxとpullが混在する場合_inboxの受信ディレクトリだけ返ること(t *testing.T) {
	// Arrange
	d := daemonWith(
		config.Topic{Name: "orders", Source: config.Source{Type: config.SourceTypeSFTP, Directory: "/out/orders"}},
		inboxTopic("invoices", "/inbox/invoices", 30),
		inboxTopic("receipts", "/inbox/receipts", 10),
	)

	// Act
	dirs := d.inboxDirs()

	// Assert
	if len(dirs) != 2 || dirs[0] != "/inbox/invoices" || dirs[1] != "/inbox/receipts" {
		t.Errorf("inboxDirs = %v, want only the two inbox directories", dirs)
	}
}

func TestInboxDirs_inboxが無い場合_空になること(t *testing.T) {
	// Arrange
	d := daemonWith(config.Topic{Name: "orders", Source: config.Source{Type: config.SourceTypeLocal, Directory: "/out/orders"}})

	// Act & Assert
	if dirs := d.inboxDirs(); len(dirs) != 0 {
		t.Errorf("inboxDirs without inbox topics must be empty, got %v", dirs)
	}
}

func TestMinFallbackInterval_複数のinboxがある場合_最小の間隔を返すこと(t *testing.T) {
	// Arrange
	d := daemonWith(
		inboxTopic("invoices", "/inbox/invoices", 30),
		inboxTopic("receipts", "/inbox/receipts", 10),
	)

	// Act & Assert
	if got := d.minFallbackInterval(); got != 10 {
		t.Errorf("minFallbackInterval = %d, want 10", got)
	}
}

func TestMinFallbackInterval_fallbackが未設定のinboxの場合_polling_intervalを使うこと(t *testing.T) {
	// Arrange (fallback=0 は applyDefaults 前の状態を模す。polling_interval を流用する)
	d := daemonWith(inboxTopic("invoices", "/inbox/invoices", 0))

	// Act & Assert
	if got := d.minFallbackInterval(); got != 60 {
		t.Errorf("minFallbackInterval = %d, want polling_interval 60", got)
	}
}
