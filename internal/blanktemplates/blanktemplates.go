// Package blanktemplates embeds small, genuinely empty Word/Excel/
// PowerPoint documents — the same shape "Blank document"/"Blank
// workbook"/"Blank presentation" produce in the real apps, generated once
// via the docx/exceljs/pptxgenjs npm packages (a one-off authoring step,
// not a build-time or runtime dependency of this app — the three files
// here are static, checked-in assets, same as internal/db/migrations'
// own .sql files). Used by ItemService.CreateBlankDocument for the file
// browser's "+ New" -> "New Word document"/"New spreadsheet"/"New
// presentation" action, which needs *some* real bytes to hand OnlyOffice
// before there's anything to open an editor on at all.
package blanktemplates

import (
	"embed"
	"io/fs"
)

//go:embed blank.docx blank.xlsx blank.pptx
var files embed.FS

// MimeTypes maps each supported extension to its real OOXML content type
// — used both by CreateBlankDocument (the new item's own MimeType) and by
// the handler validating a request's "type" against this same set.
var MimeTypes = map[string]string{
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// Bytes returns the blank template for ext ("docx"/"xlsx"/"pptx"), or
// false if ext isn't one of those three.
func Bytes(ext string) ([]byte, bool) {
	if _, ok := MimeTypes[ext]; !ok {
		return nil, false
	}
	b, err := fs.ReadFile(files, "blank."+ext)
	if err != nil {
		return nil, false
	}
	return b, true
}
