package thumbnail

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestSupported(t *testing.T) {
	for _, ext := range []string{"jpg", "jpeg", "png", "pdf"} {
		if !Supported(ext) {
			t.Errorf("Supported(%q) = false, want true", ext)
		}
	}
	for _, ext := range []string{"gif", "webp", "docx", "mp4", ""} {
		if Supported(ext) {
			t.Errorf("Supported(%q) = true, want false", ext)
		}
	}
}

func TestGenerate_ImageLargerThanMax_IsDownscaledAndDecodable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writePNG(t, path, 800, 400) // 2:1, larger than maxDimension on both axes

	data, err := Generate(path, "png")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode generated thumbnail: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != maxDimension || b.Dy() != maxDimension/2 {
		t.Errorf("thumbnail size = %dx%d, want %dx%d", b.Dx(), b.Dy(), maxDimension, maxDimension/2)
	}
}

func TestGenerate_ImageSmallerThanMax_IsNotUpscaled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tiny.png")
	writePNG(t, path, 40, 20)

	data, err := Generate(path, "png")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode generated thumbnail: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 40 || b.Dy() != 20 {
		t.Errorf("thumbnail size = %dx%d, want 40x20 (no upscaling)", b.Dx(), b.Dy())
	}
}

func TestGenerate_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.zip")
	if err := os.WriteFile(path, []byte("not really a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(path, "zip"); err != ErrUnsupported {
		t.Errorf("Generate(zip) error = %v, want ErrUnsupported", err)
	}
}

// TestGenerate_PDF only runs when pdftoppm is actually on PATH (it is in
// the Docker runtime image, per the Dockerfile, but not necessarily in
// every dev/CI environment) — same guard textextract's own PDF/OCR tests
// use, since there's nothing meaningful to assert without the real binary.
func TestGenerate_PDF(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not installed, skipping")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	// Minimal single blank-page PDF, just enough for pdftoppm to rasterize.
	minimalPDF := "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]>>endobj\n" +
		"trailer<</Root 1 0 R>>\n"
	if err := os.WriteFile(path, []byte(minimalPDF), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := Generate(path, "pdf")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(data) == 0 {
		t.Error("Generate(pdf) returned no bytes")
	}
}
