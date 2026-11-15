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
	ErrLimit       = errors.New("xlsreader: size limit exceeded")
)

func invalid(message string) error     { return fmt.Errorf("%w: %s", ErrInvalidFile, message) }
func unsupported(message string) error { return fmt.Errorf("%w: %s", ErrUnsupported, message) }

func openFile(filename string) (*os.File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("xlsreader: open: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("xlsreader: stat: %w", err)
	}
	if info.Size() > MaxFileSize {
		f.Close()
		return nil, ErrLimit
	}
	return f, nil
}

func readInput(r io.Reader) ([]byte, error) {
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
	return data, nil
}
