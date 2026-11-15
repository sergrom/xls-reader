package xlsreader

import "unicode/utf16"

func decodeUTF16Bytes(data []byte) (string, error) {
	if len(data)%2 != 0 {
		return "", invalid("odd UTF-16 byte length")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = le.Uint16(data[2*i:])
	}
	return decodeUTF16(units)
}

func decodeUTF16(units []uint16) (string, error) {
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u >= 0xD800 && u <= 0xDBFF {
			if i+1 == len(units) || units[i+1] < 0xDC00 || units[i+1] > 0xDFFF {
				return "", invalid("unpaired UTF-16 high surrogate")
			}
			i++
		} else if u >= 0xDC00 && u <= 0xDFFF {
			return "", invalid("unpaired UTF-16 low surrogate")
		}
	}
	return string(utf16.Decode(units)), nil
}

// SST string headers cross records without an extra byte. Only a continuation
// of character data starts with a new encoding flag, which may change encoding.
type sstCursor struct {
	blocks     [][]byte
	block, pos int
}

func (c *sstCursor) rawByte() (byte, error) {
	for c.block < len(c.blocks) && c.pos == len(c.blocks[c.block]) {
		c.block++
		c.pos = 0
	}
	if c.block >= len(c.blocks) {
		return 0, invalid("truncated shared string table")
	}
	b := c.blocks[c.block][c.pos]
	c.pos++
	return b, nil
}
func (c *sstCursor) uint16() (uint16, error) {
	a, err := c.rawByte()
	if err != nil {
		return 0, err
	}
	b, err := c.rawByte()
	return uint16(a) | uint16(b)<<8, err
}
func (c *sstCursor) uint32() (uint32, error) {
	a, err := c.uint16()
	if err != nil {
		return 0, err
	}
	b, err := c.uint16()
	return uint32(a) | uint32(b)<<16, err
}

func readSST(blocks [][]byte) ([]string, error) {
	if len(blocks) == 0 || len(blocks[0]) < 8 {
		return nil, invalid("missing SST header")
	}
	available := 0
	for _, block := range blocks {
		if len(block) == 0 {
			return nil, invalid("empty SST continuation")
		}
		available += len(block)
	}
	c := sstCursor{blocks: blocks}
	total, _ := c.uint32()
	unique, _ := c.uint32()
	if unique > total || uint64(unique) > uint64((available-8)/3) {
		return nil, invalid("invalid SST string count")
	}
	result := make([]string, 0)
	for i := uint32(0); i < unique; i++ {
		value, err := c.unicodeString()
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if c.block != len(blocks)-1 || c.pos != len(blocks[c.block]) {
		return nil, invalid("unexpected data after SST strings")
	}
	return result, nil
}

// unicodeString reads XLUnicodeRichExtendedString. Formatting runs and phonetic
// metadata are skipped after the text; unlike characters, they do not have an
// encoding byte when they continue into another record.
func (c *sstCursor) unicodeString() (string, error) {
	length, err := c.uint16()
	if err != nil {
		return "", err
	}
	if length > 32767 {
		return "", invalid("string exceeds cell text limit")
	}
	flag, err := c.rawByte()
	if err != nil {
		return "", err
	}
	if flag & ^byte(0x0D) != 0 {
		return "", invalid("invalid Unicode string flags")
	}
	var runs uint16
	var extension uint32
	if flag&8 != 0 {
		runs, err = c.uint16()
		if err != nil {
			return "", err
		}
	}
	if flag&4 != 0 {
		extension, err = c.uint32()
		if err != nil {
			return "", err
		}
	}
	wide := flag&1 != 0
	units := make([]uint16, 0, int(length))
	for len(units) < int(length) {
		if c.block >= len(c.blocks) {
			return "", invalid("truncated character data")
		}
		if c.pos == len(c.blocks[c.block]) {
			c.block++
			c.pos = 0
			if c.block >= len(c.blocks) || len(c.blocks[c.block]) == 0 {
				return "", invalid("missing string CONTINUE")
			}
			flag = c.blocks[c.block][0]
			c.pos = 1
			if flag > 1 {
				return "", invalid("invalid continuation encoding")
			}
			wide = flag == 1
		}
		width := 1
		if wide {
			width = 2
		}
		if len(c.blocks[c.block])-c.pos < width {
			return "", invalid("UTF-16 code unit split across records")
		}
		unit := uint16(c.blocks[c.block][c.pos])
		if wide {
			unit = le.Uint16(c.blocks[c.block][c.pos:])
		}
		c.pos += width
		units = append(units, unit)
	}
	if err := c.skip(uint64(runs)*4 + uint64(extension)); err != nil {
		return "", err
	}
	return decodeUTF16(units)
}

func (c *sstCursor) skip(count uint64) error {
	for count > 0 {
		for c.block < len(c.blocks) && c.pos == len(c.blocks[c.block]) {
			c.block++
			c.pos = 0
		}
		if c.block >= len(c.blocks) {
			return invalid("truncated rich string metadata")
		}
		n := uint64(len(c.blocks[c.block]) - c.pos)
		if n > count {
			n = count
		}
		c.pos += int(n)
		count -= n
	}
	return nil
}

func readSingleString(blocks [][]byte) (string, error) {
	c := sstCursor{blocks: blocks}
	value, err := c.unicodeString()
	if err != nil {
		return "", err
	}
	if c.block != len(blocks)-1 || c.pos != len(blocks[c.block]) {
		return "", invalid("unexpected data after string")
	}
	return value, nil
}
