package pdf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestRenderPoC confirms the chosen library (go-pdf/fpdf) produces a valid PDF
// containing Spanish text and the euro sign. Writing a sample file is opt-in via
// PDF_POC_OUT so the PoC can be eyeballed without cluttering normal test runs.
func TestRenderPoC(t *testing.T) {
	b, err := renderPoC()
	if err != nil {
		t.Fatalf("renderPoC: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Fatalf("output is not a PDF (prefix %q)", b[:min(8, len(b))])
	}
	if len(b) < 800 {
		t.Fatalf("PDF suspiciously small: %d bytes", len(b))
	}
	if out := os.Getenv("PDF_POC_OUT"); out != "" {
		path := filepath.Join(out, "poc.pdf")
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatalf("writing sample: %v", err)
		}
		t.Logf("PoC PDF escrito en %s (%d bytes)", path, len(b))
	}
}
