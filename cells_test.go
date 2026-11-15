package xlsreader

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func numericCell(col uint16, value float64) []byte {
	b := make([]byte, 14)
	le.PutUint16(b[2:], col)
	le.PutUint64(b[6:], math.Float64bits(value))
	return rec(recordNumber, b)
}
func rkCell(col uint16, value uint32) []byte {
	b := make([]byte, 10)
	le.PutUint16(b[2:], col)
	le.PutUint32(b[6:], value)
	return rec(recordRK, b)
}
func boolCell(col uint16, value, isError byte) []byte {
	b := make([]byte, 8)
	le.PutUint16(b[2:], col)
	b[6] = value
	b[7] = isError
	return rec(recordBoolErr, b)
}
func formulaCell(col uint16, number float64, kind int, special byte) []byte {
	b := make([]byte, 22)
	le.PutUint16(b[2:], col)
	if kind < 0 {
		le.PutUint64(b[6:], math.Float64bits(number))
	} else {
		b[6] = byte(kind)
		b[8] = special
		le.PutUint16(b[12:], 0xFFFF)
	}
	return rec(recordFormula, b)
}

func TestCellConversions(t *testing.T) {
	negative := int32(-25)
	tests := []struct {
		record []byte
		want   string
	}{
		{numericCell(0, 42), "42"}, {numericCell(0, -12.5), "-12.5"},
		{numericCell(0, 0), "0"}, {numericCell(0, math.Copysign(0, -1)), "0"},
		{numericCell(0, 0.0000001), "0.0000001"},
		{numericCell(0, math.NaN()), ""}, {numericCell(0, math.Inf(1)), ""},
		{rkCell(0, 42<<2|2), "42"}, {rkCell(0, uint32(negative)<<2|2), "-25"},
		{rkCell(0, 12345<<2|3), "123.45"}, {rkCell(0, uint32(negative)<<2|3), "-0.25"},
		{rkCell(0, uint32(math.Float64bits(1.5)>>32)), "1.5"},
		{rkCell(0, uint32(math.Float64bits(150)>>32)|1), "1.5"},
		{boolCell(0, 1, 0), "TRUE"}, {boolCell(0, 0, 0), "FALSE"}, {boolCell(0, 2, 0), ""},
		{boolCell(0, 7, 1), "#DIV/0!"}, {boolCell(0, 0x2A, 1), "#N/A"},
		{boolCell(0, 0xFF, 1), ""}, {boolCell(0, 1, 3), ""},
		{formulaCell(0, 2.5, -1, 0), "2.5"}, {formulaCell(0, 0, 1, 1), "TRUE"},
		{formulaCell(0, 0, 1, 0), "FALSE"}, {formulaCell(0, 0, 2, 0x17), "#REF!"},
		{formulaCell(0, 0, 3, 0), ""}, {formulaCell(0, 0, 0x99, 0), ""},
		{formulaCell(0, 0, 0, 0), ""}, // No cached STRING: leave blank, do not evaluate.
		{rec(recordRString, make([]byte, 6)), ""},
	}
	for i, tc := range tests {
		rows, err := readWorkbook(workbookFixture(nil, dimensions(1, 1), tc.record), 0)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if !reflect.DeepEqual(rows, [][]string{{tc.want}}) {
			t.Fatalf("case %d: %v want %q", i, rows, tc.want)
		}
	}
}

func TestMultipleCells(t *testing.T) {
	b := make([]byte, 18)
	le.PutUint16(b[2:], 1)
	le.PutUint32(b[6:], 12<<2|2)
	le.PutUint32(b[12:], 250<<2|3)
	le.PutUint16(b[16:], 2)
	blanks := make([]byte, 10)
	le.PutUint16(blanks[2:], 3)
	le.PutUint16(blanks[8:], 4)
	rows, err := readWorkbook(workbookFixture(nil, dimensions(1, 5), rec(recordMulRK, b), rec(recordMulBlank, blanks)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, [][]string{{"", "12", "2.5", "", ""}}) {
		t.Fatal(rows)
	}
}

func TestFormulaStrings(t *testing.T) {
	rows, err := readWorkbook(workbookFixture(nil, dimensions(1, 4),
		formulaCell(0, 0, 0, 0), rec(recordString, []byte{2, 0, 0, 'o', 'k'}),
		formulaCell(1, 0, 0, 0), rec(0x04BC, []byte{0, 0}), // Shared-formula metadata may intervene.
		rec(recordString, []byte{3, 0, 0, 'a'}), rec(recordContinue, []byte{1, 0x3D, 0xD8, 0x27, 0xDC}),
		formulaCell(2, 0, 0, 0), numericCell(3, 9),
		rec(recordString, []byte{3, 0, 0, 'b', 'a', 'd'}), // Must not attach to a previous formula.
	), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, [][]string{{"ok", "a🐧", "", "9"}}) {
		t.Fatal(rows)
	}
}

func TestInlineAndRichText(t *testing.T) {
	// One formatting run and two phonetic bytes, split after character data.
	first := append(sharedStringsHeader(2, 2), 2, 0, 0x0C, 1, 0, 2, 0, 0, 0, 'h', 'i')
	second := []byte{0, 0, 1, 0, 0xAB, 0xCD, 1, 0, 0, 'x'}
	sst := append(rec(recordSST, first), rec(recordContinue, second)...)
	inline := make([]byte, 6)
	le.PutUint16(inline[2:], 2)
	inline = append(inline, 2, 0, 0, 'o', 'k')
	rows, err := readWorkbook(workbookFixture(sst, dimensions(1, 3), label(0, 0, 0), label(0, 1, 1), rec(recordLabel, inline)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, [][]string{{"hi", "x", "ok"}}) {
		t.Fatal(rows)
	}
}

func TestIgnoredFeaturesDoNotShiftCells(t *testing.T) {
	b := make([]byte, 6)
	le.PutUint16(b[2:], 2)
	rows, err := readWorkbook(workbookFixture(rec(0xFFFF, nil), dimensions(1, 4),
		numericCell(0, 1), rec(0xFFFF, []byte{255, 255, 255, 255}), rec(recordRString, b),
		bof(0x0020), numericCell(0, 999), rec(recordEOF, nil), numericCell(3, 4),
	), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows, [][]string{{"1", "", "", "4"}}) {
		t.Fatal(rows)
	}
}

func TestTruncatedCellsRemainErrors(t *testing.T) {
	for _, id := range []uint16{recordNumber, recordRK, recordBoolErr, recordFormula, recordLabel, recordMulRK, recordMulBlank} {
		_, err := readWorkbook(workbookFixture(nil, dimensions(1, 1), rec(id, make([]byte, 6))), 0)
		if !errors.Is(err, ErrInvalidFile) {
			t.Fatalf("record %04x: %v", id, err)
		}
	}
	_, err := readSST([][]byte{append(sharedStringsHeader(1, 1), 1, 0, 8, 1, 0, 'x')})
	if !errors.Is(err, ErrInvalidFile) {
		t.Fatal("missing formatting run", err)
	}
}

func TestIndependentWriter(t *testing.T) {
	rows, err := ReadXLS("testdata/mixed-xlwt.xls", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"City", "42", "-12.5", "TRUE", "FALSE", "001", "🐧"},
		{"123.45", "1.23456789012345", "45292", "", "rich text", "after formula"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("got %#v want %#v", rows, want)
	}
}
