package xlsreader

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

const (
	endOfChain  uint32 = 0xFFFFFFFE
	freeSector  uint32 = 0xFFFFFFFF
	fatSector   uint32 = 0xFFFFFFFD
	difatSector uint32 = 0xFFFFFFFC
	sectorSize         = 512
)

var le = binary.LittleEndian
var oleSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

type compoundFile struct {
	data    []byte
	sectors int
	fat     []uint32
	used    []bool
}

func workbookStream(data []byte) ([]byte, error) {
	if len(data) < 8 || !bytes.Equal(data[:8], oleSignature) {
		return nil, unsupported("expected OLE/CFB .xls (not XLSX, CSV, or raw BIFF)")
	}
	if len(data) < 512 {
		return nil, invalid("truncated OLE header")
	}
	if le.Uint16(data[26:]) != 3 || le.Uint16(data[30:]) != 9 {
		return nil, unsupported("only CFB version 3 with 512-byte sectors is supported")
	}
	if le.Uint16(data[28:]) != 0xFFFE || le.Uint16(data[32:]) != 6 || le.Uint32(data[56:]) != 4096 {
		return nil, invalid("invalid CFB byte order, mini sector size, or cutoff")
	}
	if len(data)%512 != 0 || len(data) < 1536 {
		return nil, invalid("truncated OLE sectors")
	}
	c := &compoundFile{data: data, sectors: len(data)/512 - 1}
	c.used = make([]bool, c.sectors)
	fatCount, difatCount := le.Uint32(data[44:]), le.Uint32(data[72:])
	if fatCount == 0 || uint64(fatCount) > uint64(c.sectors) || uint64(difatCount) > uint64(c.sectors) {
		return nil, invalid("invalid FAT/DIFAT count")
	}
	ids := make([]uint32, 0)
	addFAT := func(id uint32) error {
		if id == freeSector {
			return nil
		}
		if uint64(id) >= uint64(c.sectors) || c.used[id] || len(ids) >= int(fatCount) {
			return invalid("invalid or duplicate FAT sector")
		}
		c.used[id] = true
		ids = append(ids, id)
		return nil
	}
	for pos := 76; pos < 512; pos += 4 {
		if err := addFAT(le.Uint32(data[pos:])); err != nil {
			return nil, err
		}
	}
	next := le.Uint32(data[68:])
	difatIDs := make([]uint32, 0)
	for i := uint32(0); i < difatCount; i++ {
		if uint64(next) >= uint64(c.sectors) || c.used[next] {
			return nil, invalid("invalid or cyclic DIFAT chain")
		}
		c.used[next] = true
		difatIDs = append(difatIDs, next)
		block := c.sector(next)
		for pos := 0; pos < 508; pos += 4 {
			if err := addFAT(le.Uint32(block[pos:])); err != nil {
				return nil, err
			}
		}
		next = le.Uint32(block[508:])
	}
	if next != endOfChain || len(ids) != int(fatCount) {
		return nil, invalid("DIFAT length does not match header")
	}
	for _, id := range ids {
		block := c.sector(id)
		for pos := 0; pos < 512; pos += 4 {
			c.fat = append(c.fat, le.Uint32(block[pos:]))
		}
	}
	if len(c.fat) < c.sectors {
		return nil, invalid("FAT does not cover all sectors")
	}
	for _, id := range ids {
		if c.fat[id] != fatSector {
			return nil, invalid("FAT sector marker missing")
		}
	}
	for _, id := range difatIDs {
		if c.fat[id] != difatSector {
			return nil, invalid("DIFAT sector marker missing")
		}
	}
	directory, err := c.chain(le.Uint32(data[48:]), -1)
	if err != nil {
		return nil, fmt.Errorf("directory: %w", err)
	}
	if len(directory) < 128 || directory[66] != 5 {
		return nil, invalid("missing root directory entry")
	}
	root := directory[:128]
	// Walk only siblings of the root's children: nested embedded workbooks are
	// not substitutes for the document's own Workbook stream.
	stack := []uint32{le.Uint32(root[76:])}
	visited := make(map[uint32]bool)
	var workbook []byte
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == freeSector {
			continue
		}
		if uint64(id) >= uint64(len(directory)/128) || visited[id] {
			return nil, invalid("invalid or cyclic directory tree")
		}
		visited[id] = true
		entry := directory[int(id)*128 : int(id+1)*128]
		if entry[66] != 1 && entry[66] != 2 {
			return nil, invalid("invalid directory child type")
		}
		stack = append(stack, le.Uint32(entry[68:]), le.Uint32(entry[72:]))
		length := int(le.Uint16(entry[64:]))
		if length < 2 || length > 64 || length%2 != 0 || le.Uint16(entry[length-2:]) != 0 {
			return nil, invalid("invalid directory name")
		}
		name, err := decodeUTF16Bytes(entry[:length-2])
		if err != nil {
			return nil, err
		}
		if entry[66] == 2 && (strings.EqualFold(name, "Workbook") || strings.EqualFold(name, "Book")) {
			if workbook != nil {
				return nil, invalid("multiple workbook streams")
			}
			workbook = entry
		}
	}
	if workbook == nil {
		return nil, invalid("Workbook stream not found")
	}
	size, err := streamSize(workbook, len(data))
	if err != nil {
		return nil, err
	}
	start := le.Uint32(workbook[116:])
	if size >= 4096 {
		return c.chain(start, size)
	}
	miniCount := le.Uint32(data[64:])
	if uint64(miniCount) > uint64(c.sectors) {
		return nil, invalid("invalid mini FAT size")
	}
	miniBytes, err := c.chain(le.Uint32(data[60:]), int(miniCount)*512)
	if err != nil {
		return nil, err
	}
	rootSize, err := streamSize(root, len(data))
	if err != nil {
		return nil, err
	}
	miniStream, err := c.chain(le.Uint32(root[116:]), rootSize)
	if err != nil {
		return nil, err
	}
	if rootSize%64 != 0 {
		return nil, invalid("misaligned mini stream")
	}
	if size == 0 {
		return nil, invalid("empty workbook stream")
	}
	result := make([]byte, 0, size)
	seen := make(map[uint32]bool)
	for remaining := size; remaining > 0; remaining -= 64 {
		if uint64(start) >= uint64(len(miniBytes)/4) || uint64(start) >= uint64(len(miniStream)/64) || seen[start] {
			return nil, invalid("invalid or cyclic mini FAT chain")
		}
		seen[start] = true
		take := 64
		if remaining < take {
			take = remaining
		}
		offset := int(start) * 64
		result = append(result, miniStream[offset:offset+take]...)
		start = le.Uint32(miniBytes[int(start)*4:])
	}
	if start != endOfChain {
		return nil, invalid("mini stream chain exceeds declared size")
	}
	return result, nil
}

func streamSize(entry []byte, fileSize int) (int, error) {
	size := le.Uint64(entry[120:])
	if size > uint64(fileSize) {
		return 0, invalid("stream size exceeds file size")
	}
	return int(size), nil
}

// sector is called only after the sector number has been validated.
func (c *compoundFile) sector(id uint32) []byte {
	offset := (int(id) + 1) * 512
	return c.data[offset : offset+512]
}

// chain reads a normal FAT chain. size == -1 means an unknown directory size.
// A shared allocation map catches loops and overlapping metadata/data streams.
func (c *compoundFile) chain(start uint32, size int) ([]byte, error) {
	result := make([]byte, 0)
	for start != endOfChain {
		if uint64(start) >= uint64(c.sectors) || c.used[start] {
			return nil, invalid("invalid, cyclic, or overlapping FAT chain")
		}
		if size >= 0 && len(result) >= size {
			return nil, invalid("FAT chain exceeds declared stream size")
		}
		c.used[start] = true
		block := c.sector(start)
		if size >= 0 && size-len(result) < 512 {
			block = block[:size-len(result)]
		}
		result = append(result, block...)
		start = c.fat[start]
	}
	if size >= 0 && len(result) != size {
		return nil, invalid("truncated FAT stream")
	}
	return result, nil
}
