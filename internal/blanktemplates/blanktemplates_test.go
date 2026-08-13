package blanktemplates

import (
	"archive/zip"
	"bytes"
	"testing"
)

// A hand-generated, checked-in binary asset (see this package's own doc
// comment) is exactly the kind of thing that can silently rot — wrong
// file committed, truncated copy, wrong extension — with nothing else in
// the build catching it. This doesn't parse the full OOXML schema, just
// confirms each blob really is a zip archive containing the one file
// every OOXML document (docx/xlsx/pptx alike) must have.
func TestBytes_EachTemplateIsARealZipWithContentTypes(t *testing.T) {
	for ext := range MimeTypes {
		content, ok := Bytes(ext)
		if !ok {
			t.Fatalf("Bytes(%q) reported not found", ext)
		}
		if len(content) == 0 {
			t.Fatalf("Bytes(%q) is empty", ext)
		}
		zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
		if err != nil {
			t.Fatalf("blank.%s is not a valid zip: %v", ext, err)
		}
		found := false
		for _, f := range zr.File {
			if f.Name == "[Content_Types].xml" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("blank.%s has no [Content_Types].xml — not a real OOXML document", ext)
		}
	}
}

func TestBytes_UnsupportedExtension(t *testing.T) {
	if _, ok := Bytes("pdf"); ok {
		t.Error(`Bytes("pdf") should report not found — there's no blank template for it`)
	}
}
