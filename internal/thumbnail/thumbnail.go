// Package thumbnail turns an image or PDF on disk into a small JPEG preview
// — best-effort, same spirit as internal/textextract: an unsupported type or
// a missing pdftoppm just means no thumbnail (the frontend falls back to its
// generic per-type icon, see FileIcon.svelte), never a hard failure of the
// file operation that triggered it. See ItemService.Thumbnail, the only
// caller.
package thumbnail

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"  // also self-registers as a decoder, covering the jpg/jpeg input case
	_ "image/png" // decode support only, registered via side-effect import
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// maxDimension bounds the thumbnail's longer side — plenty for a grid tile
// (see routes/+page.svelte's .item-tile, ~9.5rem wide) without bloating the
// on-disk cache (see ItemService.Thumbnail) for something never shown above
// a few hundred CSS pixels.
const maxDimension = 320

// jpegQuality trades size for fidelity — a thumbnail is a UI cue, not an
// archival copy, so a fairly aggressive quality keeps the cache small.
const jpegQuality = 78

// ImageExts mirrors textextract.OCRImageExts's own reasoning (same
// leptonica/tesseract-adjacent format set the rest of this app already
// commits to) — kept as its own list rather than importing textextract's
// just to avoid a dependency between two otherwise-unrelated packages for
// one shared constant.
var ImageExts = map[string]bool{"jpg": true, "jpeg": true, "png": true}

// Supported reports whether ext (no leading dot, lowercase — see
// textextract.ExtFor) is a type Generate knows how to thumbnail.
func Supported(ext string) bool {
	return ImageExts[ext] || ext == "pdf"
}

// ErrUnsupported mirrors textextract.ErrUnsupported.
var ErrUnsupported = errors.New("thumbnail: unsupported file type")

// Generate returns JPEG-encoded thumbnail bytes for the file at path.
func Generate(path, ext string) ([]byte, error) {
	switch {
	case ImageExts[ext]:
		return generateFromImage(path)
	case ext == "pdf":
		return generateFromPDF(path)
	default:
		return nil, ErrUnsupported
	}
}

func generateFromImage(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return encode(img)
}

// pdftoppmPath mirrors textextract's own private lookup (each package
// resolves it independently rather than sharing one — see this package's
// doc comment) — missing means "no PDF thumbnails", not a startup failure.
var pdftoppmPath = sync.OnceValue(func() string {
	p, err := exec.LookPath("pdftoppm")
	if err != nil {
		return ""
	}
	return p
})

// generateFromPDF rasterizes only the first page (a thumbnail only ever
// needs one representative image) at a modest resolution — plenty for
// maxDimension, and much cheaper than the 200dpi/30-page pass
// textextract.ExtractOCRPDF does for actual text recognition.
func generateFromPDF(path string) ([]byte, error) {
	bin := pdftoppmPath()
	if bin == "" {
		return nil, ErrUnsupported
	}

	dir, err := os.MkdirTemp("", "denizen-thumb-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	prefix := filepath.Join(dir, "page")
	cmd := exec.Command(bin, "-jpeg", "-r", "100", "-singlefile", "-f", "1", "-l", "1", path, prefix)
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	f, err := os.Open(prefix + ".jpg")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return encode(img)
}

func encode(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resizeToFit(img, maxDimension), &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// resizeToFit downscales img so its longer side is maxDim, preserving
// aspect ratio — a no-op if img is already smaller (never upscale a small
// image just to hit the target). Nearest-neighbor sampling: visibly
// aliased under close inspection, invisible at grid-tile size, and needs no
// dependency beyond the standard library.
// ponytail: nearest-neighbor, not a filtered resize — upgrade to
// golang.org/x/image/draw's CatmullRom if a thumbnail's softness/aliasing
// ever becomes a real complaint.
func resizeToFit(img image.Image, maxDim int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		return img
	}

	var nw, nh int
	if w >= h {
		nw = maxDim
		nh = max(1, h*maxDim/w)
	} else {
		nh = maxDim
		nw = max(1, w*maxDim/h)
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	return dst
}
