package xlsreader

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Workbook holds document properties and all worksheets in tab order.
// Open and Parse eagerly read all worksheets. Mutations are not synchronized.
type Workbook struct {
	Properties Properties
	Sheets     []*Sheet
}

// Properties contains optional SummaryInformation metadata. Missing strings
// are empty; missing or zero FILETIME dates are zero time.Time values.
// Timestamps are returned in UTC.
type Properties struct {
	Title          string
	Subject        string
	Creator        string
	Keywords       string
	Description    string
	LastModifiedBy string
	CreatedAt      time.Time
	ModifiedAt     time.Time
}

// Sheet describes a worksheet. Index is its original zero-based tab index.
// Name, Index and Visibility are snapshots of metadata from the XLS file.
type Sheet struct {
	Name       string
	Index      int
	Visibility SheetVisibility
	rows       [][]string
}

// SheetVisibility describes the visibility stored in the worksheet tab record.
type SheetVisibility uint8

// SheetVeryHidden cannot be made visible through Excel's ordinary Unhide menu.
const (
	SheetVisible SheetVisibility = iota
	SheetHidden
	SheetVeryHidden
)

// Rows returns the parsed rows without copying. Treat the returned slices as
// read-only; changing them changes this Sheet. No I/O occurs and no errors are
// deferred until this call. Missing cells are empty strings; entirely empty rows
// are nil slices. Rows end at their last recognized cell, and trailing empty
// rows from DIMENSIONS are preserved. Numbers, booleans, Excel errors and cached
// formula results are converted to strings. Unsupported values remain blank.
// Number formats are not applied: dates are returned as numeric serial values.
func (s *Sheet) Rows() [][]string { return s.rows }

// SheetByName finds the first worksheet whose name matches case-insensitively.
func (w *Workbook) SheetByName(name string) (*Sheet, bool) {
	for _, sheet := range w.Sheets {
		if sheet != nil && strings.EqualFold(sheet.Name, name) {
			return sheet, true
		}
	}
	return nil, false
}

// Open reads all worksheets and optional document properties, then closes the
// file. It returns no partial Workbook on failure.
func Open(filename string) (*Workbook, error) {
	f, err := openFile(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a workbook without closing r. All worksheets and supported
// properties are decoded before returning. Input buffering is bounded by
// MaxFileSize; decoded worksheets require additional memory.
func Parse(r io.Reader) (*Workbook, error) {
	data, err := readInput(r)
	if err != nil {
		return nil, err
	}
	c, err := openCompound(data)
	if err != nil {
		return nil, err
	}
	stream, err := c.readStream(c.workbook)
	if err != nil {
		return nil, err
	}
	globals, err := readGlobals(stream)
	if err != nil {
		return nil, err
	}
	book := &Workbook{Sheets: globals.sheets}
	for index, sheet := range book.Sheets {
		sheet.rows, err = readSelectedSheet(stream, globals.end, globals.offsets, globals.strings, index)
		if err != nil {
			return nil, fmt.Errorf("xlsreader: sheet %d (%q): %w", index, sheet.Name, err)
		}
	}
	if c.summary != nil {
		summary, err := c.readStream(c.summary)
		if err != nil {
			return nil, fmt.Errorf("xlsreader: properties: %w", err)
		}
		book.Properties, err = readProperties(summary)
		if err != nil {
			return nil, fmt.Errorf("xlsreader: properties: %w", err)
		}
	}
	return book, nil
}
