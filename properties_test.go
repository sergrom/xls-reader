package xlsreader

import (
	"bytes"
	"errors"
	"sort"
	"testing"
	"time"
	"unicode/utf16"
)

func propertyFixture(values map[uint32][]byte) []byte {
	ids := make([]int, 0, len(values))
	for id := range values {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	section := make([]byte, 8+len(ids)*8)
	le.PutUint32(section[4:], uint32(len(ids)))
	for i, id := range ids {
		le.PutUint32(section[8+i*8:], uint32(id))
		le.PutUint32(section[12+i*8:], uint32(len(section)))
		section = append(section, values[uint32(id)]...)
		for len(section)%4 != 0 {
			section = append(section, 0)
		}
	}
	le.PutUint32(section, uint32(len(section)))
	header := make([]byte, 48)
	le.PutUint16(header, 0xFFFE)
	le.PutUint32(header[24:], 1)
	copy(header[28:], summaryFMTID)
	le.PutUint32(header[44:], 48)
	return append(header, section...)
}
func cpProperty(cp uint16) []byte {
	v := make([]byte, 8)
	le.PutUint32(v, 2)
	le.PutUint16(v[4:], cp)
	return v
}
func textProperty(kind uint32, encoded []byte, length uint32) []byte {
	v := make([]byte, 8)
	le.PutUint32(v, kind)
	le.PutUint32(v[4:], length)
	return append(v, encoded...)
}
func utf16Property(s string) []byte {
	units := utf16.Encode([]rune(s + "\x00"))
	b := make([]byte, len(units)*2)
	for i, u := range units {
		le.PutUint16(b[i*2:], u)
	}
	return textProperty(31, b, uint32(len(units)))
}
func timeProperty(t time.Time) []byte {
	ticks := uint64(t.Unix()+11644473600)*10000000 + uint64(t.Nanosecond()/100)
	b := make([]byte, 12)
	le.PutUint32(b, 0x40)
	le.PutUint64(b[4:], ticks)
	return b
}

func TestPropertyEncodingsAndTimes(t *testing.T) {
	stamp := time.Date(2024, 6, 5, 4, 3, 2, 123456700, time.UTC)
	cases := []struct {
		cp    uint16
		title []byte
		want  string
	}{
		{1252, textProperty(30, []byte{'c', 'a', 'f', 0xE9, 0}, 5), "café"},
		{1251, textProperty(30, []byte{0xCF, 0xF0, 0xE8, 0xE2, 0xE5, 0xF2, 0}, 7), "Привет"},
		{65001, textProperty(30, []byte("🐧\x00"), 5), "🐧"},
		{1200, textProperty(30, []byte{'A', 0, 0, 0}, 4), "A"},
		{1252, utf16Property("Hello 🐧"), "Hello 🐧"},
		{9999, textProperty(30, []byte("text\x00"), 5), ""},
	}
	for _, tc := range cases {
		data := propertyFixture(map[uint32][]byte{1: cpProperty(tc.cp), 2: tc.title, 12: timeProperty(stamp), 13: timeProperty(stamp.Add(time.Hour))})
		p, err := readProperties(data)
		if err != nil {
			t.Fatal(err)
		}
		if p.Title != tc.want || !p.CreatedAt.Equal(stamp) || !p.ModifiedAt.Equal(stamp.Add(time.Hour)) {
			t.Fatalf("%d: %+v", tc.cp, p)
		}
		if p.Subject != "" {
			t.Fatal("absent field")
		}
	}
	data := propertyFixture(map[uint32][]byte{2: {3, 0, 0, 0, 1, 0, 0, 0}, 12: {0x40, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}})
	p, err := readProperties(data)
	if err != nil || p != (Properties{}) {
		t.Fatal(p, err)
	}
}

func TestMalformedProperties(t *testing.T) {
	good := propertyFixture(map[uint32][]byte{1: cpProperty(1200), 2: utf16Property("text")})
	for _, n := range []int{0, 27, 47, 49, len(good) - 1} {
		if _, err := readProperties(good[:n]); !errors.Is(err, ErrInvalidFile) {
			t.Fatal(n, err)
		}
	}
	mutations := []func([]byte){
		func(b []byte) { le.PutUint32(b[24:], 0xFFFFFFFF) },
		func(b []byte) { le.PutUint32(b[44:], 0xFFFFFFFF) },
		func(b []byte) { le.PutUint32(b[48:], 0xFFFFFFFF) },
		func(b []byte) { le.PutUint32(b[52:], 0xFFFFFFFF) },
		func(b []byte) { le.PutUint32(b[60:], 0) },
		func(b []byte) { le.PutUint32(b[68:], le.Uint32(b[60:])) },
	}
	for i, mutate := range mutations {
		data := bytes.Clone(good)
		mutate(data)
		if _, err := readProperties(data); !errors.Is(err, ErrInvalidFile) {
			t.Fatal(i, err)
		}
	}
	for _, value := range [][]byte{textProperty(31, []byte{0}, 0xFFFFFFFF), {0x40, 0, 0, 0}} {
		id := uint32(2)
		if value[0] == 0x40 {
			id = 12
		}
		if _, err := readProperties(propertyFixture(map[uint32][]byte{id: value})); !errors.Is(err, ErrInvalidFile) {
			t.Fatal(err)
		}
	}
}

func FuzzProperties(f *testing.F) {
	f.Add(propertyFixture(map[uint32][]byte{1: cpProperty(1200), 2: utf16Property("title 🐧"), 12: timeProperty(time.Unix(0, 0))}))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = readProperties(data)
	})
}
