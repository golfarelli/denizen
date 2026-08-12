// Package textextract turns a file on disk into plain text for the search
// index (internal/repository/search.go) — best-effort, never a hard
// dependency of the upload/edit path that calls it: an extraction failure
// (corrupt file, unsupported subtype, pdftotext missing) just means that
// file won't turn up in a content search, it never blocks the file
// operation itself. See ItemService.indexContent, the only caller.
package textextract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxSourceBytes caps how much of a file we'll even attempt to read/convert
// — a multi-gigabyte video or disk image accidentally named .txt shouldn't
// make an upload hang extracting "text" from it forever.
const maxSourceBytes = 50 << 20 // 50MB

// maxExtractedRunes caps what actually lands in the FTS5 index per file —
// even a legitimately huge document only needs "enough text to match a
// search query against", not its full, unbounded content sitting in the
// database forever.
const maxExtractedRunes = 2 << 20 // ~2M characters

// Supported reports whether ext (no leading dot, lowercase — see
// strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))) is a type
// Extract knows how to handle at all. Callers use this to skip the
// extraction attempt entirely for types that will only ever return
// ErrUnsupported (images, video, archives, ...) — cheaper than trying and
// catching the error every time.
func Supported(ext string) bool {
	switch ext {
	case "txt", "md", "csv", "json", "pdf", "docx", "xlsx", "pptx":
		return true
	}
	return false
}

// ErrUnsupported is returned for any extension Supported reports false
// for — kept distinct from other errors so a caller could, in principle,
// tell "nothing to extract here" apart from "tried and failed", though
// today's only caller (ItemService.indexContent) treats every error the
// same (best-effort, log and move on).
var ErrUnsupported = errors.New("textextract: unsupported file type")

// Extract returns plain text pulled from path, dispatched by ext (see
// Supported). Truncates to maxExtractedRunes rather than erroring on a
// huge document.
func Extract(path, ext string) (string, error) {
	var text string
	var err error
	switch ext {
	case "txt", "md", "csv", "json":
		text, err = extractPlain(path)
	case "pdf":
		text, err = extractPDF(path)
	case "docx":
		text, err = extractZippedXML(path, func(name string) bool { return name == "word/document.xml" })
	case "pptx":
		text, err = extractZippedXML(path, isSlideXML)
	case "xlsx":
		text, err = extractZippedXML(path, func(name string) bool { return name == "xl/sharedStrings.xml" })
	default:
		return "", ErrUnsupported
	}
	if err != nil {
		return "", err
	}
	return truncateRunes(text, maxExtractedRunes), nil
}

func extractPlain(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxSourceBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// extractPDF shells out to poppler-utils' pdftotext (present in the
// runtime image — see Dockerfile) rather than pulling in a Go PDF-parsing
// dependency: it's a small, well-tested, single-purpose binary already
// the standard tool for this, and this app already shells out for the
// same reason elsewhere (ScanDialog's server-side PDF assembly). Missing
// pdftotext degrades to "PDFs aren't searchable", not a startup failure —
// checked once, not on every call, since exec.LookPath itself touches the
// filesystem.
var pdftotextPath = sync.OnceValue(func() string {
	p, err := exec.LookPath("pdftotext")
	if err != nil {
		return ""
	}
	return p
})

func extractPDF(path string) (string, error) {
	bin := pdftotextPath()
	if bin == "" {
		return "", ErrUnsupported
	}
	// "-" (stdout) instead of a temp .txt file — no cleanup to forget, and
	// this whole path already assumes small-to-medium personal files, not
	// something so large that buffering it all in memory matters.
	cmd := exec.Command(bin, "-layout", path, "-")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// maxOCRPages caps how many pages of a scanned PDF get OCR'd — OCR is
// orders of magnitude slower than pdftotext (whole seconds per page on
// modest hardware), so an unusually long scanned document is truncated
// rather than tying up the background OCR sweep (ItemService.RunOCRSweep)
// on one file for minutes. A search index only needs enough of the
// document to be findable, not every page.
const maxOCRPages = 30

// ocrTimeout bounds the whole OCR pass for one file — a corrupt or
// pathological PDF (e.g. one page rendering to an enormous bitmap)
// shouldn't be able to wedge the sweep indefinitely.
const ocrTimeout = 5 * time.Minute

// pdftoppmPath/tesseractPath mirror pdftotextPath below: resolved once,
// missing means "OCR isn't available", not a startup failure — RunOCRSweep
// simply marks every candidate attempted-with-nothing-found until poppler-
// utils/tesseract are actually installed (see Dockerfile).
var pdftoppmPath = sync.OnceValue(func() string {
	p, err := exec.LookPath("pdftoppm")
	if err != nil {
		return ""
	}
	return p
})

var tesseractPath = sync.OnceValue(func() string {
	p, err := exec.LookPath("tesseract")
	if err != nil {
		return ""
	}
	return p
})

// ExtractOCRPDF is extractPDF's fallback for a scanned PDF — one with no
// real text layer, so pdftotext comes back empty (see
// ItemService.RunOCRSweep, the only caller: it only ever tries this for a
// PDF that already came up text-less). Rasterizes each page to a PNG via
// pdftoppm, then runs tesseract against each page image in turn,
// concatenating the results — slow (real image recognition, not just
// parsing embedded text), which is exactly why this runs from a
// background sweep instead of indexContent's synchronous upload path.
// Italian + English word lists (see Dockerfile) since this app's real
// documents are mostly Italian with the occasional English one.
func ExtractOCRPDF(path string) (string, error) {
	ppmBin := pdftoppmPath()
	tessBin := tesseractPath()
	if ppmBin == "" || tessBin == "" {
		return "", ErrUnsupported
	}

	dir, err := os.MkdirTemp("", "denizen-ocr-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	ctx, cancel := context.WithTimeout(context.Background(), ocrTimeout)
	defer cancel()

	prefix := filepath.Join(dir, "page")
	rasterize := exec.CommandContext(ctx, ppmBin,
		"-png", "-r", "200", "-l", strconv.Itoa(maxOCRPages), path, prefix)
	if err := rasterize.Run(); err != nil {
		return "", err
	}

	pages, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return "", err
	}
	sort.Strings(pages) // pdftoppm zero-pads per run, so this is already page order

	var b strings.Builder
	for _, page := range pages {
		cmd := exec.CommandContext(ctx, tessBin, page, "-", "-l", "ita+eng")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			continue // one unreadable page shouldn't blank out the rest of the document
		}
		b.Write(out.Bytes())
		b.WriteByte('\n')
	}
	return b.String(), nil
}

var slideXMLPattern = regexp.MustCompile(`^ppt/slides/slide\d+\.xml$`)

func isSlideXML(name string) bool { return slideXMLPattern.MatchString(name) }

// extractZippedXML reads every entry in path (a zip archive — .docx/.xlsx/
// .pptx all are, per the Office Open XML format) whose name matches want,
// and concatenates every XML character-data token found in each — a
// generic "get me the text" pass that works across all three formats
// without needing format-specific tag knowledge (a Word paragraph's
// <w:t>, a slide's <a:t>, a shared string's <t> are all just character
// data to an XML decoder). Loses structure (paragraphs, cell boundaries,
// slide order within a deck) — irrelevant for a search index, which only
// ever needs "does this text appear in this file", not a faithful export.
func extractZippedXML(path string, want func(name string) bool) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	// Sorted so a multi-slide .pptx's extracted text reads in slide order
	// rather than whatever order the zip directory happens to list them —
	// not load-bearing for search matching, just nicer if this text is
	// ever inspected directly.
	var names []string
	for _, f := range r.File {
		if want(f.Name) {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		rc, err := r.Open(name)
		if err != nil {
			continue // one damaged part shouldn't blank out the rest of the document
		}
		text, err := extractXMLCharData(rc)
		rc.Close()
		if err != nil {
			continue
		}
		b.WriteString(text)
		b.WriteByte(' ')
	}
	return b.String(), nil
}

func extractXMLCharData(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A part that's well-formed up to some point still has usable
			// text before the error — return what was gathered instead of
			// discarding it, same "best effort" spirit as the rest of this
			// package.
			return b.String(), nil
		}
		if cd, ok := tok.(xml.CharData); ok {
			b.Write(cd)
			b.WriteByte(' ')
		}
	}
	return b.String(), nil
}

func truncateRunes(s string, max int) string {
	if len(s) <= max { // fast path: byte length is always >= rune count
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// ExtFor is the one-line normalization every caller needs from a filename
// before checking Supported/calling Extract — pulled out so
// ItemService.indexContent and this package's own tests share exactly one
// definition of "the extension" instead of two copies drifting apart.
func ExtFor(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}
