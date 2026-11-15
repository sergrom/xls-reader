// Package xlsreader reads BIFF8 XLS worksheets as strings. It supports common
// cell types and ignores values it cannot convert. Formulas are not evaluated.
package xlsreader

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// MaxFileSize bounds input buffering. Decoded strings and rows require additional
// memory. Files are read into memory; this is not a streaming worksheet reader.
const MaxFileSize = 128 << 20

var (
	ErrInvalidFile = errors.New("xlsreader: invalid XLS file")
	ErrUnsupported = errors.New("xlsreader: unsupported XLS feature")
	ErrSheetIndex  = errors.New("xlsreader: sheet index out of range")
	ErrLimit       = errors.New("xlsreader: size limit exceeded")
)

// ReadXLS opens filename, reads a zero-based sheet, and closes the file.
func ReadXLS(filename string, sheetIndex int) ([][]string, error) {
	if sheetIndex < 0 {
		return nil, ErrSheetIndex
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("xlsreader: open: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("xlsreader: stat: %w", err)
	}
	if info.Size() > MaxFileSize {
		return nil, ErrLimit
	}
	return Read(f, sheetIndex)
}

// Read reads a zero-based sheet without closing r. Text, numbers, booleans,
// Excel errors and cached formula results are converted to strings. Unsupported
// values are left blank and unknown BIFF records are ignored. Row/column positions
// and trailing blank rows from DIMENSIONS are preserved; rows extend through
// their last recognized cell. Entirely empty rows are nil slices.
// Number formats are not applied: dates are returned as numeric serial values.
// Encryption and unsupported file formats still return ErrUnsupported; malformed
// data still returns ErrInvalidFile. Only the selected sheet's cells are parsed.
func Read(r io.Reader, sheetIndex int) ([][]string, error) {
	if sheetIndex < 0 {
		return nil, ErrSheetIndex
	}
	if r == nil {
		return nil, fmt.Errorf("%w: nil reader", ErrInvalidFile)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("xlsreader: read: %w", err)
	}
	if len(data) > MaxFileSize {
		return nil, ErrLimit
	}
	stream, err := workbookStream(data)
	if err != nil {
		return nil, err
	}
	return readWorkbook(stream, sheetIndex)
}

func invalid(message string) error     { return fmt.Errorf("%w: %s", ErrInvalidFile, message) }
func unsupported(message string) error { return fmt.Errorf("%w: %s", ErrUnsupported, message) }
