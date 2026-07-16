package smspdu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return strings.TrimSpace(string(b))
}

func TestParseInternational(t *testing.T) {
	d, err := ParseHexDeliver(loadFixture(t, "msg0_international.hex"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.From != "+79956009754" {
		t.Fatalf("from = %q, want +79956009754", d.From)
	}
	if d.Text == "" {
		t.Fatal("empty text")
	}
	if !strings.Contains(d.Text, "Uzbekistan") {
		t.Fatalf("text = %q", d.Text)
	}
}

func TestParseBeelineConcatUCS2Fixture(t *testing.T) {
	d, err := ParseHexDeliver(loadFixture(t, "beeline_concat_ucs2_part2.hex"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.ConcatRef != 82 || d.Part != 2 || d.PartTotal != 9 {
		t.Fatalf("concat ref=%d part=%d total=%d", d.ConcatRef, d.Part, d.PartTotal)
	}
	if d.From != "BeeRouming" {
		t.Fatalf("from = %q", d.From)
	}
}

func TestParseConcatUCS2(t *testing.T) {
	d, err := ParseHexDeliver(loadFixture(t, "msg1_concat_ucs2.hex"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.ConcatRef != 82 || d.Part != 2 || d.PartTotal != 9 {
		t.Fatalf("concat ref=%d part=%d total=%d", d.ConcatRef, d.Part, d.PartTotal)
	}
	if d.From != "BeeRouming" {
		t.Fatalf("from = %q", d.From)
	}
	if !strings.Contains(strings.ToLower(d.Text), "rouming") {
		t.Fatalf("text = %q", d.Text)
	}
}

func TestParseShortCode(t *testing.T) {
	d, err := ParseHexDeliver(loadFixture(t, "msg34_shortcode.hex"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.From == "" || strings.HasPrefix(d.From, "6610110") {
		t.Fatalf("from = %q", d.From)
	}
	if d.Text == "" {
		t.Fatal("empty text")
	}
}
