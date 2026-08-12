package flow

import (
	"testing"
	"time"
)

// TestOCRSweepFlow_MarksAttemptedSoItDoesntRetryForever covers the one
// piece of RunOCRSweep that's actually non-trivial: not the OCR itself
// (that's poppler-utils/tesseract's job, and this test environment has
// neither installed — see textextract.ExtractOCRPDF's own doc comment on
// degrading to a no-op when they're missing), but the bookkeeping that
// stops the sweep from re-attempting the same file forever. This upload
// isn't a real PDF, so pdftotext extracts nothing from it either — no FTS
// row lands for it at all, exactly the same "empty" state a genuinely
// scanned PDF (pdftotext "succeeds" but with only page-break characters)
// leaves behind, and both are OCRRepository.ListPending candidates.
func TestOCRSweepFlow_MarksAttemptedSoItDoesntRetryForever(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	uploadFile(t, ts, fabio, nil, "scan.pdf", []byte("not actually a pdf"))

	attempted, err := ts.app.Items.RunOCRSweep(ctx, 10)
	if err != nil {
		t.Fatalf("RunOCRSweep: %v", err)
	}
	if attempted != 1 {
		t.Fatalf("first sweep attempted = %d, want 1 (the fake scan)", attempted)
	}

	attempted, err = ts.app.Items.RunOCRSweep(ctx, 10)
	if err != nil {
		t.Fatalf("RunOCRSweep (second): %v", err)
	}
	if attempted != 0 {
		t.Fatalf("second sweep attempted = %d, want 0 (already marked attempted)", attempted)
	}
}
