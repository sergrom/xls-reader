package xlsreader

import (
	"bytes"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var summaryFMTID = []byte{0xE0, 0x85, 0x9F, 0xF2, 0xF9, 0x4F, 0x68, 0x10, 0xAB, 0x91, 0x08, 0x00, 0x2B, 0x27, 0xB3, 0xD9}

// readProperties reads the standard SummaryInformation property set. Unknown
// property IDs/types and unsupported code pages are ignored, leaving zero values.
// Structural corruption still returns ErrInvalidFile.
func readProperties(data []byte) (Properties, error) {
	var result Properties
	if len(data) < 28 || le.Uint16(data) != 0xFFFE {
		return result, invalid("invalid property stream header")
	}
	sets := uint64(le.Uint32(data[24:]))
	if sets == 0 || sets > uint64((len(data)-28)/20) {
		return result, invalid("invalid property set count")
	}
	headerEnd := 28 + int(sets)*20
	for i := 0; i < int(sets); i++ {
		header := data[28+i*20 : 28+(i+1)*20]
		if !bytes.Equal(header[:16], summaryFMTID) {
			continue
		}
		offset := uint64(le.Uint32(header[16:]))
		if offset < uint64(headerEnd) || offset+8 > uint64(len(data)) {
			return result, invalid("invalid property set offset")
		}
		section := data[int(offset):]
		size := uint64(le.Uint32(section))
		count := uint64(le.Uint32(section[4:]))
		if size < 8 || size > uint64(len(section)) || count > (size-8)/8 {
			return result, invalid("invalid property set size")
		}
		section = section[:int(size)]
		type entry struct {
			id     uint32
			offset int
		}
		entries := make([]entry, 0, int(count))
		seen := make(map[uint32]bool)
		for j := 0; j < int(count); j++ {
			id := le.Uint32(section[8+j*8:])
			pos := uint64(le.Uint32(section[12+j*8:]))
			if seen[id] || pos < 8+count*8 || pos+4 > size {
				return result, invalid("invalid property table entry")
			}
			seen[id] = true
			entries = append(entries, entry{id, int(pos)})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].offset < entries[j].offset })
		values := make(map[uint32][]byte)
		for j, e := range entries {
			end := len(section)
			if j+1 < len(entries) {
				end = entries[j+1].offset
			}
			if end-e.offset < 4 {
				return result, invalid("overlapping property values")
			}
			values[e.id] = section[e.offset:end]
		}
		codePage := uint16(1252)
		if value := values[1]; len(value) > 0 && le.Uint32(value) == 2 {
			if len(value) < 6 {
				return result, invalid("truncated property code page")
			}
			codePage = le.Uint16(value[4:])
		}
		fields := map[uint32]*string{2: &result.Title, 3: &result.Subject, 4: &result.Creator, 5: &result.Keywords, 6: &result.Description, 8: &result.LastModifiedBy}
		for id, target := range fields {
			value, err := propertyString(values[id], codePage)
			if err != nil {
				return Properties{}, err
			}
			*target = value
		}
		var err error
		result.CreatedAt, err = propertyTime(values[12])
		if err != nil {
			return Properties{}, err
		}
		result.ModifiedAt, err = propertyTime(values[13])
		if err != nil {
			return Properties{}, err
		}
		return result, nil
	}
	return result, nil
}

func propertyString(value []byte, codePage uint16) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	if len(value) < 4 {
		return "", invalid("truncated property value")
	}
	kind := le.Uint32(value)
	if kind != 0x1E && kind != 0x1F {
		return "", nil
	}
	if len(value) < 8 {
		return "", invalid("truncated property string")
	}
	length := uint64(le.Uint32(value[4:]))
	if kind == 0x1F {
		length *= 2
	}
	if length > uint64(len(value)-8) {
		return "", invalid("property string exceeds value size")
	}
	content := value[8 : 8+int(length)]
	if kind == 0x1F || codePage == 1200 {
		text, err := decodeUTF16Bytes(content)
		return strings.TrimRight(text, "\x00"), err
	}
	content = bytes.TrimRight(content, "\x00")
	if codePage == 65001 {
		if !utf8.Valid(content) {
			return "", invalid("invalid UTF-8 property string")
		}
		return string(content), nil
	}
	switch codePage {
	case 1251, 1252, 28591, 20127:
		runes := make([]rune, len(content))
		for i, b := range content {
			switch {
			case codePage == 1251 && b >= 128:
				runes[i] = cp1251[b-128]
			case codePage == 1252 && b >= 128 && b < 160:
				runes[i] = cp1252[b-128]
			case codePage == 20127 && b >= 128:
				return "", nil
			default:
				runes[i] = rune(b)
			}
		}
		return string(runes), nil
	default:
		return "", nil
	}
}

func propertyTime(value []byte) (time.Time, error) {
	if len(value) == 0 {
		return time.Time{}, nil
	}
	if len(value) < 4 {
		return time.Time{}, invalid("truncated timestamp property")
	}
	if le.Uint32(value) != 0x40 {
		return time.Time{}, nil
	}
	if len(value) < 12 {
		return time.Time{}, invalid("truncated FILETIME")
	}
	ticks := le.Uint64(value[4:])
	if ticks == 0 {
		return time.Time{}, nil
	}
	const ticksPerSecond = 10000000
	const unixEpochOffset = 11644473600
	seconds := int64(ticks/ticksPerSecond) - unixEpochOffset
	nanoseconds := int64(ticks%ticksPerSecond) * 100
	return time.Unix(seconds, nanoseconds).UTC(), nil
}

var cp1251 = [...]rune{
	0x402, 0x403, 0x201a, 0x453, 0x201e, 0x2026, 0x2020, 0x2021,
	0x20ac, 0x2030, 0x409, 0x2039, 0x40a, 0x40c, 0x40b, 0x40f,
	0x452, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014,
	0xfffd, 0x2122, 0x459, 0x203a, 0x45a, 0x45c, 0x45b, 0x45f,
	0xa0, 0x40e, 0x45e, 0x408, 0xa4, 0x490, 0xa6, 0xa7,
	0x401, 0xa9, 0x404, 0xab, 0xac, 0xad, 0xae, 0x407,
	0xb0, 0xb1, 0x406, 0x456, 0x491, 0xb5, 0xb6, 0xb7,
	0x451, 0x2116, 0x454, 0xbb, 0x458, 0x405, 0x455, 0x457,
	0x410, 0x411, 0x412, 0x413, 0x414, 0x415, 0x416, 0x417,
	0x418, 0x419, 0x41a, 0x41b, 0x41c, 0x41d, 0x41e, 0x41f,
	0x420, 0x421, 0x422, 0x423, 0x424, 0x425, 0x426, 0x427,
	0x428, 0x429, 0x42a, 0x42b, 0x42c, 0x42d, 0x42e, 0x42f,
	0x430, 0x431, 0x432, 0x433, 0x434, 0x435, 0x436, 0x437,
	0x438, 0x439, 0x43a, 0x43b, 0x43c, 0x43d, 0x43e, 0x43f,
	0x440, 0x441, 0x442, 0x443, 0x444, 0x445, 0x446, 0x447,
	0x448, 0x449, 0x44a, 0x44b, 0x44c, 0x44d, 0x44e, 0x44f,
}

var cp1252 = [...]rune{
	0x20ac, 0xfffd, 0x201a, 0x192, 0x201e, 0x2026, 0x2020, 0x2021,
	0x2c6, 0x2030, 0x160, 0x2039, 0x152, 0xfffd, 0x17d, 0xfffd,
	0xfffd, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014,
	0x2dc, 0x2122, 0x161, 0x203a, 0x153, 0xfffd, 0x17e, 0x178,
}
