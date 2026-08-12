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
// stops the sweep from re-attempting the same file forever. Covers both
// candidate kinds (OCRRepository.ListPending): a scanned PDF (pdftotext
// "succeeds" but extracts only page-break characters — this upload isn't
// a real PDF at all, so pdftotext finds nothing either, landing in the
// exact same empty state) and a photographed document (a .jpg, which has
// no separate text-extraction path to even attempt — see
// textextract.Supported — so it's always a candidate until OCR'd).
func TestOCRSweepFlow_MarksAttemptedSoItDoesntRetryForever(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	uploadFile(t, ts, fabio, nil, "scan.pdf", []byte("not actually a pdf"))
	uploadFile(t, ts, fabio, nil, "photo.jpg", []byte("not actually a jpg"))

	attempted, err := ts.app.Items.RunOCRSweep(ctx, 10)
	if err != nil {
		t.Fatalf("RunOCRSweep: %v", err)
	}
	if attempted != 2 {
		t.Fatalf("first sweep attempted = %d, want 2 (the fake scan + the fake photo)", attempted)
	}

	attempted, err = ts.app.Items.RunOCRSweep(ctx, 10)
	if err != nil {
		t.Fatalf("RunOCRSweep (second): %v", err)
	}
	if attempted != 0 {
		t.Fatalf("second sweep attempted = %d, want 0 (both already marked attempted)", attempted)
	}
}
