package textextract

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtract_PlainText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("bolletta della luce, scadenza a settembre"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Extract(path, "txt")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got != "bolletta della luce, scadenza a settembre" {
		t.Errorf("Extract(txt) = %q", got)
	}
}

func TestExtract_Docx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.docx")
	writeZip(t, path, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="ns"><w:body><w:p><w:r><w:t>Verbale riunione</w:t></w:r></w:p><w:p><w:r><w:t>del 3 agosto</w:t></w:r></w:p></w:body></w:document>`,
		// A decoy part with the same tag name elsewhere in the zip — must
		// not leak into the extracted text, only word/document.xml should.
		"word/styles.xml": `<w:styles xmlns:w="ns"><w:t>should not appear</w:t></w:styles>`,
	})

	got, err := Extract(path, "docx")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(got, "Verbale riunione") || !strings.Contains(got, "del 3 agosto") {
		t.Errorf("Extract(docx) = %q, want it to contain both paragraphs", got)
	}
	if strings.Contains(got, "should not appear") {
		t.Errorf("Extract(docx) = %q, leaked text from a part other than word/document.xml", got)
	}
}

func TestExtract_Pptx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deck.pptx")
	writeZip(t, path, map[string]string{
		"ppt/slides/slide1.xml": `<p:sld xmlns:a="ns"><a:t>Prima slide</a:t></p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld xmlns:a="ns"><a:t>Seconda slide</a:t></p:sld>`,
	})

	got, err := Extract(path, "pptx")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(got, "Prima slide") || !strings.Contains(got, "Seconda slide") {
		t.Errorf("Extract(pptx) = %q, want both slides' text", got)
	}
}

func TestExtract_Xlsx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sheet.xlsx")
	writeZip(t, path, map[string]string{
		"xl/sharedStrings.xml": `<sst xmlns="ns"><si><t>Spesa</t></si><si><t>Importo</t></si></sst>`,
	})

	got, err := Extract(path, "xlsx")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(got, "Spesa") || !strings.Contains(got, "Importo") {
		t.Errorf("Extract(xlsx) = %q, want both shared strings", got)
	}
}

func TestExtract_UnsupportedType(t *testing.T) {
	if _, err := Extract("/does/not/matter", "png"); err != ErrUnsupported {
		t.Errorf("Extract(png) err = %v, want ErrUnsupported", err)
	}
	if Supported("png") {
		t.Error("Supported(png) = true, want false")
	}
	if !Supported("pdf") {
		t.Error("Supported(pdf) = false, want true")
	}
}

func TestExtractDocx_MalformedXMLStillReturnsWhatItGotBeforeTheError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.docx")
	writeZip(t, path, map[string]string{
		"word/document.xml": `<w:document><w:t>readable text before the break</w:t><unclosed`,
	})

	got, err := Extract(path, "docx")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(got, "readable text before the break") {
		t.Errorf("Extract(malformed docx) = %q, want the text that came before the malformed part", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	s := strings.Repeat("é", 10) // multi-byte rune, deliberately not ASCII
	got := truncateRunes(s, 3)
	if got != strings.Repeat("é", 3) {
		t.Errorf("truncateRunes = %q, want 3 runes not a byte-boundary split", got)
	}
}

func TestExtFor(t *testing.T) {
	cases := map[string]string{"Report.PDF": "pdf", "notes.txt": "txt", "no-extension": ""}
	for name, want := range cases {
		if got := ExtFor(name); got != want {
			t.Errorf("ExtFor(%q) = %q, want %q", name, got, want)
		}
	}
}
