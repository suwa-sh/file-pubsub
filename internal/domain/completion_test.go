package domain

import "testing"

func TestHasCompletionSuffix_サフィックスで終わる名前の場合_trueになること(t *testing.T) {
	// Arrange
	cases := []struct {
		name   string
		fname  string
		suffix string
		want   bool
	}{
		{"tmpで終わる場合_trueであること", "invoices_0045.csv.tmp", ".tmp", true},
		{"doneで終わる場合_trueであること", "invoices_0046.csv.done", ".done", true},
		{"カスタムpartで終わる場合_trueであること", "invoices.csv.part", ".part", true},
		{"正式名でsuffixを含まない場合_falseであること", "invoices_0045.csv", ".tmp", false},
		{"別のsuffixの場合_falseであること", "invoices.csv.done", ".tmp", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Act
			got := HasCompletionSuffix(c.fname, c.suffix)

			// Assert
			if got != c.want {
				t.Errorf("HasCompletionSuffix(%q, %q) = %v, want %v", c.fname, c.suffix, got, c.want)
			}
		})
	}
}

func TestMarkerOf_本体名とsuffixを結合した名前を返すこと(t *testing.T) {
	// Arrange
	base := "invoices_0046.csv"

	// Act
	got := MarkerOf(base, ".done")

	// Assert
	if want := "invoices_0046.csv.done"; got != want {
		t.Errorf("MarkerOf(%q, .done) = %q, want %q", base, got, want)
	}
}

func TestReadyByMarker_対応するマーカーが存在する本体だけtrueになること(t *testing.T) {
	// Arrange
	names := []string{"a.csv", "a.csv.done", "b.csv"}

	// Act
	ready := ReadyByMarker(names, ".done")

	// Assert
	if !ready["a.csv"] {
		t.Errorf("a.csv has a marker and must be ready: %v", ready)
	}
	if ready["b.csv"] {
		t.Errorf("b.csv has no marker and must not be ready: %v", ready)
	}
	if ready["a.csv.done"] {
		t.Errorf("the marker itself must never be a collection target: %v", ready)
	}
}

func TestReadyByMarker_カスタムsuffixの場合_対応するマーカーで判定すること(t *testing.T) {
	// Arrange
	names := []string{"invoices_0046.csv", "invoices_0046.csv.ok"}

	// Act
	ready := ReadyByMarker(names, ".ok")

	// Assert
	if !ready["invoices_0046.csv"] {
		t.Errorf("custom .ok marker must make the body ready: %v", ready)
	}
}

func TestReadyByMarker_マーカーだけで本体が無い場合_対象にならないこと(t *testing.T) {
	// Arrange
	names := []string{"orphan.csv.done"}

	// Act
	ready := ReadyByMarker(names, ".done")

	// Assert
	if len(ready) != 0 {
		t.Errorf("a marker without its body must yield no ready target, got %v", ready)
	}
}
