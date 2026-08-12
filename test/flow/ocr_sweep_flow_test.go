package flow

import (
	"testing"
	"time"
)

// TestOCRSweepFlow_UploadTriggersItAutomaticallyAndMarksAttempted covers
// two things together, both non-trivial: that an upload actually kicks a
// background OCR pass on its own (ItemService.TriggerOCRSweep, called from
// FinalizeUpload) instead of waiting for the next scheduled tick, and the
// bookkeeping that stops that pass from re-attempting the same file
// forever. Not the OCR itself — that's poppler-utils/tesseract's job, and
// this test environment has neither installed (see
// textextract.ExtractOCRPDF's own doc comment on degrading to a no-op
// when they're missing). Covers both candidate kinds
// (OCRRepository.ListPending): a scanned PDF (pdftotext "succeeds" but
// extracts only page-break characters — this upload isn't a real PDF at
// all, so pdftotext finds nothing either, landing in the exact same empty
// state) and a photographed document (a .jpg, which has no separate
// text-extraction path to even attempt — see textextract.Supported — so
// it's always a candidate until OCR'd).
func TestOCRSweepFlow_UploadTriggersItAutomaticallyAndMarksAttempted(t *testing.T) {
	ts := newTestServer(t)
	ctx := t.Context()

	code, created, err := ts.app.Auth.EnsureBootstrapInvite(ctx, time.Hour)
	if err != nil || !created {
		t.Fatalf("EnsureBootstrapInvite: code=%q created=%v err=%v", code, created, err)
	}
	fabio := registerAndLogin(t, ts, code, "fabio", "correct-horse-battery-staple")

	uploadFile(t, ts, fabio, nil, "scan.pdf", []byte("not actually a pdf"))
	uploadFile(t, ts, fabio, nil, "photo.jpg", []byte("not actually a jpg"))

	// Each upload above triggered its own background sweep as a side
	// effect — running in its own goroutine, so there's no single moment
	// to assert a synchronous count against. Poll instead: this same
	// RunOCRSweep call is safe to run concurrently with (or after) the
	// triggered one, since MarkAttempted/IndexContent are idempotent, so
	// it doubles as "wait for both to be handled, however that happens".
	deadline := time.Now().Add(2 * time.Second)
	for {
		attempted, err := ts.app.Items.RunOCRSweep(ctx, 10)
		if err != nil {
			t.Fatalf("RunOCRSweep: %v", err)
		}
		if attempted == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("candidates still unattempted after the deadline — the upload-triggered sweep never ran?")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Both are marked attempted now, whichever sweep actually handled
	// them — a further call finds nothing left to do.
	attempted, err := ts.app.Items.RunOCRSweep(ctx, 10)
	if err != nil {
		t.Fatalf("RunOCRSweep (final check): %v", err)
	}
	if attempted != 0 {
		t.Fatalf("final check attempted = %d, want 0 (both already marked attempted)", attempted)
	}
}
