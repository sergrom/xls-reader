package xlsreader

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	writer "github.com/sergrom/xls-reader/internal/testwriter"
)

func makeFixture(t testing.TB, sheets ...[][]string) []byte {
	t.Helper()
	w := writer.New()
	for i, rows := range sheets {
		sheet, err := w.AddSheet(fmt.Sprintf("Sheet %d 🐧", i))
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if err := sheet.AppendRow(row); err != nil {
				t.Fatal(err)
			}
		}
	}
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		sheets [][][]string
	}{
		{"empty", [][][]string{nil}},
		{"text and blanks", [][][]string{
			{{"City", "Code", "Note"}, {"Paris", "001", "=1+1"}, nil, {"", "Berlin", ""}, {"Привет 🐧", "a\x00b", "line1\nline2"}, nil},
			{{"Other sheet"}, {"é", "漢字"}}, nil,
		}},
		{"continued Unicode", [][][]string{{
			{strings.Repeat("x", 4090), strings.Repeat("🐧", 16000), strings.Repeat("я", 32767)},
			{strings.Repeat("x", 4090), "last"},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := makeFixture(t, tc.sheets...)
			book, err := Parse(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if len(book.Sheets) != len(tc.sheets) {
				t.Fatal("sheet count mismatch")
			}
			for index, want := range tc.sheets {
				got := book.Sheets[index].Rows()
				if want == nil {
					want = [][]string{}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("sheet %d mismatch: got row count %d want %d", index, len(got), len(want))
				}
			}
		})
	}
}

func TestSheetLimits(t *testing.T) {
	rows := make([][]string, 65536)
	rows[0] = []string{"first"}
	rows[65535] = make([]string, 256)
	rows[65535][255] = "last"
	got, err := Parse(bytes.NewReader(makeFixture(t, rows)))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Sheets[0].Rows(), rows) {
		t.Fatal("lost row or column positions")
	}
}

func TestColumnFormattingDoesNotExtendRows(t *testing.T) {
	w := writer.New()
	s, _ := w.AddSheet("Data")
	if err := s.SetColumnWidth(255, 20); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendRow([]string{"value"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := w.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Sheets[0].Rows(), [][]string{{"value"}}) {
		t.Fatal(got)
	}
}

func TestDIFATRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("large DIFAT fixture")
	}
	rows := make([][]string, 1000)
	for i := range rows {
		rows[i] = []string{fmt.Sprintf("%04d", i) + strings.Repeat("x", 4096)}
	}
	data := makeFixture(t, rows)
	if le.Uint32(data[72:]) == 0 {
		t.Fatal("fixture did not exercise DIFAT")
	}
	got, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Sheets[0].Rows(), rows) {
		t.Fatal("DIFAT data mismatch")
	}
}

func TestFileAndReaderErrors(t *testing.T) {
	data := makeFixture(t, [][]string{{"value"}})
	path := filepath.Join(t.TempDir(), "data.xls")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Open(path)
	if err != nil || !reflect.DeepEqual(got.Sheets[0].Rows(), [][]string{{"value"}}) {
		t.Fatal(got, err)
	}
	if _, err := Open(path + ".missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if _, err := Parse(nil); !errors.Is(err, ErrInvalidFile) {
		t.Fatal(err)
	}
	marker := errors.New("read failed")
	if _, err := Parse(failingReader{marker}); !errors.Is(err, marker) {
		t.Fatal(err)
	}
	input := &closeTrackingReader{Reader: bytes.NewReader(data)}
	if _, err := Parse(input); err != nil {
		t.Fatal(err)
	}
	if input.closed {
		t.Fatal("caller-owned reader was closed")
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "too-large.xls"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFileSize + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if _, err := Open(f.Name()); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

type closeTrackingReader struct {
	io.Reader
	closed bool
}

func (r *closeTrackingReader) Close() error { r.closed = true; return nil }

func TestCorruptOLE(t *testing.T) {
	original := makeFixture(t, [][]string{{"small"}})
	mutations := map[string]func([]byte){
		"version":           func(b []byte) { le.PutUint16(b[26:], 4) },
		"fat count":         func(b []byte) { le.PutUint32(b[44:], 0xFFFFFFFF) },
		"directory pointer": func(b []byte) { le.PutUint32(b[48:], 0x12345678) },
		"directory cycle": func(b []byte) {
			dir := le.Uint32(b[48:])
			fat := le.Uint32(b[76:])
			le.PutUint32(b[(int(fat)+1)*512+int(dir)*4:], dir)
		},
		"directory tree cycle": func(b []byte) { dir := (int(le.Uint32(b[48:])) + 1) * 512; le.PutUint32(b[dir+128+72:], 1) },
		"stream size":          func(b []byte) { dir := (int(le.Uint32(b[48:])) + 1) * 512; le.PutUint64(b[dir+128+120:], 1<<40) },
		"mini FAT cycle": func(b []byte) {
			dir := (int(le.Uint32(b[48:])) + 1) * 512
			start := le.Uint32(b[dir+128+116:])
			mini := (int(le.Uint32(b[60:])) + 1) * 512
			le.PutUint32(b[mini+int(start)*4:], start)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			data := append([]byte(nil), original...)
			mutate(data)
			if _, err := Parse(bytes.NewReader(data)); err == nil {
				t.Fatal("corrupt file accepted")
			}
		})
	}
	for _, length := range []int{0, 7, 100, 511, 512, len(original) - 1, len(original) - 512} {
		if _, err := Parse(bytes.NewReader(original[:length])); err == nil {
			t.Fatalf("accepted truncated length %d", length)
		}
	}
}
