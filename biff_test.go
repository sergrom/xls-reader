package xlsreader

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func rec(id uint16, payload []byte) []byte {
	b := make([]byte, 4+len(payload))
	le.PutUint16(b, id)
	le.PutUint16(b[2:], uint16(len(payload)))
	copy(b[4:], payload)
	return b
}
func bof(kind uint16) []byte {
	b := make([]byte, 16)
	le.PutUint16(b, 0x0600)
	le.PutUint16(b[2:], kind)
	return rec(recordBOF, b)
}
func dimensions(rows uint32, cols uint16) []byte {
	b := make([]byte, 14)
	le.PutUint32(b[4:], rows)
	le.PutUint16(b[10:], cols)
	return rec(recordDimensions, b)
}
func label(row, col uint16, index uint32) []byte {
	b := make([]byte, 10)
	le.PutUint16(b, row)
	le.PutUint16(b[2:], col)
	le.PutUint32(b[6:], index)
	return rec(recordLabelSST, b)
}
func sharedStringsHeader(total, unique uint32) []byte {
	b := make([]byte, 8)
	le.PutUint32(b, total)
	le.PutUint32(b[4:], unique)
	return b
}
func workbookFixture(globals []byte, cells ...[]byte) []byte {
	sheet := bof(0x0010)
	for _, cell := range cells {
		sheet = append(sheet, cell...)
	}
	sheet = append(sheet, rec(recordEOF, nil)...)
	global := append(bof(0x0005), globals...)
	bound := make([]byte, 9)
	bound[6] = 1
	bound[8] = 'S'
	le.PutUint32(bound, uint32(len(global)+4+len(bound)+4))
	global = append(global, rec(recordBoundSheet, bound)...)
	global = append(global, rec(recordEOF, nil)...)
	return append(global, sheet...)
}

func TestSSTEncodings(t *testing.T) {
	cases := []struct {
		name   string
		blocks [][]byte
		want   []string
	}{
		{"compressed", [][]byte{append(sharedStringsHeader(1, 1), 3, 0, 0, 'a', 0xE9, 'z')}, []string{"aéz"}},
		{"encoding switch", [][]byte{
			append(sharedStringsHeader(1, 1), 3, 0, 0, 'a'),
			{1, 0x3D, 0xD8, 0x27, 0xDC},
		}, []string{"a🐧"}},
		{"surrogate across continuation", [][]byte{
			append(sharedStringsHeader(1, 1), 2, 0, 1, 0x3D, 0xD8), {1, 0x27, 0xDC},
		}, []string{"🐧"}},
		{"string starts at next record", [][]byte{
			append(sharedStringsHeader(2, 2), 1, 0, 0, 'a'), {1, 0, 0, 'b'},
		}, []string{"a", "b"}},
		{"empty string", [][]byte{append(sharedStringsHeader(1, 1), 0, 0, 1)}, []string{""}},
		{"header split", [][]byte{append(sharedStringsHeader(1, 1), 1), {0, 0, 'a'}}, []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readSST(tc.blocks)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatal(got, tc.want)
			}
		})
	}
}

func TestMalformedSST(t *testing.T) {
	cases := [][][]byte{
		nil, {{0}}, {sharedStringsHeader(0, 1)},
		{append(sharedStringsHeader(1, 1), 1, 0, 1, 0)},
		{append(sharedStringsHeader(1, 1), 2, 0, 1, 'a', 0)},
		{append(sharedStringsHeader(1, 1), 1, 0, 1, 0x00, 0xD8)},
		{append(sharedStringsHeader(1, 1), 1, 0, 1, 0x00, 0xDC)},
		{append(sharedStringsHeader(1, 1), 2, 0, 0, 'a'), {2, 'b'}},
		{sharedStringsHeader(0, 0), {}},
		{append(sharedStringsHeader(0, 0), 1)},
	}
	for i, blocks := range cases {
		if _, err := readSST(blocks); !errors.Is(err, ErrInvalidFile) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if _, err := readSST([][]byte{append(sharedStringsHeader(1, 1), 1, 0, 8, 'a')}); !errors.Is(err, ErrInvalidFile) {
		t.Fatal(err)
	}
}

func TestBIFFSparseCells(t *testing.T) {
	sst := rec(recordSST, append(sharedStringsHeader(1, 1), 1, 0, 0, 'x'))
	data := workbookFixture(sst, dimensions(4, 4), label(2, 3, 0))
	rows, err := readWorkbook(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, [][]string{nil, nil, {"", "", "", "x"}, nil}) {
		t.Fatal(rows)
	}
}

func TestUnsupportedFormatsAndIgnoredRecords(t *testing.T) {
	_, err := readWorkbook(workbookFixture(rec(0xFFFF, nil), dimensions(0, 0), rec(0xFFFF, []byte{1, 2, 3})), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = readWorkbook(workbookFixture(rec(0x002F, nil), dimensions(0, 0)), 0)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	old := workbookFixture(nil, dimensions(0, 0))
	le.PutUint16(old[4:], 0x0500)
	if _, err := readWorkbook(old, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestMalformedBIFF(t *testing.T) {
	sst := rec(recordSST, append(sharedStringsHeader(1, 1), 1, 0, 0, 'x'))
	cases := [][]byte{
		workbookFixture(sst, label(0, 0, 0)),
		workbookFixture(sst, dimensions(1, 1), label(0, 0, 1)),
		workbookFixture(sst, dimensions(1, 1), label(1, 0, 0)),
		workbookFixture(sst, dimensions(1, 1), label(0, 1, 0)),
		workbookFixture(sst, dimensions(65537, 1)),
		workbookFixture(sst, dimensions(1, 257)),
		workbookFixture(sst, dimensions(1, 1), dimensions(1, 1)),
	}
	valid := workbookFixture(sst, dimensions(1, 1), label(0, 0, 0))
	cases = append(cases, valid[:len(valid)-4], valid[:len(valid)-1])
	for i, data := range cases {
		if _, err := readWorkbook(data, 0); !errors.Is(err, ErrInvalidFile) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	// Fuzz-style single-byte mutations must produce either a result or an error,
	// never an unchecked bounds panic or a chain traversal loop.
	for i := range valid {
		mutated := bytes.Clone(valid)
		mutated[i] ^= 0xFF
		_, _ = readWorkbook(mutated, 0)
	}
}

func FuzzBIFF(f *testing.F) {
	f.Add(workbookFixture(rec(recordSST, append(sharedStringsHeader(1, 1), 1, 0, 0, 'x')), dimensions(1, 1), label(0, 0, 0)))
	f.Add(workbookFixture(nil, dimensions(1, 3), numericCell(0, 1.25), formulaCell(1, 0, 0, 0), rec(recordString, []byte{1, 0, 0, 'x'}), boolCell(2, 1, 0)))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = readWorkbook(data, 0)
	})
}
