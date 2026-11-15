package xlsreader

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	writer "github.com/sergrom/xls-reader/internal/testwriter"
)

func rewriteWorkbook(t *testing.T, file []byte, edit func([]byte)) []byte {
	t.Helper()
	c, err := openCompound(file)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := c.readStream(c.workbook)
	if err != nil {
		t.Fatal(err)
	}
	offset := bytes.Index(file, stream)
	if offset < 0 {
		t.Fatal("test fixture workbook is not contiguous")
	}
	edit(stream)
	result := bytes.Clone(file)
	copy(result[offset:], stream)
	return result
}

func TestWorkbookAPI(t *testing.T) {
	data := makeFixture(t, [][]string{{"first", "001"}}, [][]string{nil, {"second"}}, nil)
	data = rewriteWorkbook(t, data, func(stream []byte) {
		r := biffReader{data: stream}
		n := byte(0)
		for {
			record, err := r.next()
			if err != nil {
				t.Fatal(err)
			}
			if record.id == recordEOF {
				break
			}
			if record.id == recordBoundSheet {
				record.data[4] = n
				n++
			}
		}
	})
	book, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(book.Sheets) != 3 {
		t.Fatal(len(book.Sheets))
	}
	for i, sheet := range book.Sheets {
		if sheet.Index != i || sheet.Visibility != SheetVisibility(i) {
			t.Fatal(sheet)
		}
		got, ok := book.SheetByName(strings.ToUpper(sheet.Name))
		if !ok || got != sheet {
			t.Fatal("name lookup", sheet.Name)
		}
	}
	if sheet, ok := book.SheetByName("missing"); ok || sheet != nil {
		t.Fatal("unexpected match")
	}
	path := filepath.Join(t.TempDir(), "book.xls")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opened, book) {
		t.Fatal("Open and Parse differ")
	}
	if _, err := Open(path + ".missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	input := &closeTrackingReader{Reader: bytes.NewReader(data)}
	if _, err := Parse(input); err != nil {
		t.Fatal(err)
	}
	if input.closed {
		t.Fatal("Parse closed reader")
	}
	if _, err := Parse(nil); !errors.Is(err, ErrInvalidFile) {
		t.Fatal(err)
	}
	marker := errors.New("read failure")
	if _, err := Parse(failingReader{marker}); !errors.Is(err, marker) {
		t.Fatal(err)
	}
}

func TestWorkbookProperties(t *testing.T) {
	for _, largeBook := range []bool{false, true} {
		for _, largeSummary := range []bool{false, true} {
			w := writer.New()
			expected := writer.Properties{Title: "Report 🐧", Subject: "Cities", Creator: "Сергей", Keywords: "one, two", Description: "Description", LastModifiedBy: "Émilie"}
			if largeSummary {
				expected.Description = strings.Repeat("long 🐧", 1000)
			}
			w.SetProperties(expected)
			s, err := w.AddSheet("Données 🐧")
			if err != nil {
				t.Fatal(err)
			}
			text := "value"
			if largeBook {
				text = strings.Repeat("abc", 2000)
			}
			if err := s.AppendRow([]string{text}); err != nil {
				t.Fatal(err)
			}
			var buffer bytes.Buffer
			before := time.Now().Add(-time.Second)
			if _, err := w.WriteTo(&buffer); err != nil {
				t.Fatal(err)
			}
			book, err := Parse(&buffer)
			if err != nil {
				t.Fatalf("large workbook=%v summary=%v: %v", largeBook, largeSummary, err)
			}
			p := book.Properties
			if p.Title != expected.Title || p.Subject != expected.Subject || p.Creator != expected.Creator || p.Keywords != expected.Keywords || p.Description != expected.Description || p.LastModifiedBy != expected.LastModifiedBy {
				t.Fatal("properties mismatch")
			}
			for _, stamp := range []time.Time{p.CreatedAt, p.ModifiedAt} {
				if stamp.Before(before) || stamp.After(time.Now()) || stamp.Location() != time.UTC {
					t.Fatal("invalid timestamp", stamp)
				}
			}
			sheet, ok := book.SheetByName("données 🐧")
			if !ok || sheet.Rows()[0][0] != text {
				t.Fatal("unicode name or rows mismatch")
			}
		}
	}
}

func TestOptionalProperties(t *testing.T) {
	// xlwt fixture has no SummaryInformation stream.
	book, err := Open("testdata/mixed-xlwt.xls")
	if err != nil {
		t.Fatal(err)
	}
	if book.Properties != (Properties{}) {
		t.Fatal(book.Properties)
	}
	if len(book.Sheets) != 1 || book.Sheets[0].Name != "Mixed" {
		t.Fatal(book.Sheets)
	}
}

func TestEagerParsing(t *testing.T) {
	data := makeFixture(t, [][]string{{"good"}}, [][]string{{"bad"}})
	data = rewriteWorkbook(t, data, func(stream []byte) {
		globals, err := readGlobals(stream)
		if err != nil {
			t.Fatal(err)
		}
		// Make the second worksheet's BOF invalid.
		le.PutUint16(stream[globals.offsets[1]:], 0xFFFF)
	})

	if book, err := Parse(bytes.NewReader(data)); book != nil || !errors.Is(err, ErrInvalidFile) {
		t.Fatal(book, err)
	}
}

func TestInvalidVisibility(t *testing.T) {
	data := makeFixture(t, [][]string{{"x"}})
	data = rewriteWorkbook(t, data, func(stream []byte) {
		r := biffReader{data: stream}
		for {
			rec, err := r.next()
			if err != nil {
				t.Fatal(err)
			}
			if rec.id == recordBoundSheet {
				rec.data[4] = 3
				break
			}
		}
	})
	if _, err := Parse(bytes.NewReader(data)); !errors.Is(err, ErrInvalidFile) {
		t.Fatal(err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("not an XLS"))
	f.Add(makeFixture(f, [][]string{{"value", "🐧"}, nil, {""}}))
	f.Add(makeFixture(f, [][]string{{"value", "🐧"}}, [][]string{{"other"}}))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = Parse(bytes.NewReader(data))
	})
}

func TestMalformedSummary(t *testing.T) {
	data := makeFixture(t, [][]string{{"value"}})
	c, err := openCompound(data)
	if err != nil {
		t.Fatal(err)
	}
	if c.summary == nil {
		t.Fatal("missing fixture metadata")
	}
	summary, err := c.readStream(c.summary)
	if err != nil {
		t.Fatal(err)
	}
	pos := bytes.Index(data, summary)
	if pos < 0 {
		t.Fatal("noncontiguous test summary")
	}
	changed := bytes.Clone(data)
	le.PutUint32(changed[pos+44:], 0xFFFFFFFF)

	if book, err := Parse(bytes.NewReader(changed)); book != nil || !errors.Is(err, ErrInvalidFile) {
		t.Fatal(book, err)
	}
}

func TestMiniStreamOverlap(t *testing.T) {
	data := makeFixture(t, [][]string{{"small"}})
	changed := bytes.Clone(data)
	directory := (int(le.Uint32(changed[48:])) + 1) * 512
	// Both streams are in the root mini stream. Alias the metadata to the workbook.
	copy(changed[directory+2*128+116:directory+2*128+128], changed[directory+128+116:directory+128+128])
	if _, err := Parse(bytes.NewReader(changed)); !errors.Is(err, ErrInvalidFile) {
		t.Fatal(err)
	}
}
