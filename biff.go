package xlsreader

const (
	recordBOF        = 0x0809
	recordEOF        = 0x000A
	recordSST        = 0x00FC
	recordContinue   = 0x003C
	recordBoundSheet = 0x0085
	recordDimensions = 0x0200
	recordLabelSST   = 0x00FD
	recordBlank      = 0x0201
)

type biffRecord struct {
	id     uint16
	data   []byte
	offset int
}
type biffReader struct {
	data []byte
	pos  int
}

func (r *biffReader) next() (biffRecord, error) {
	if len(r.data)-r.pos < 4 {
		return biffRecord{}, invalid("truncated BIFF record header or missing EOF")
	}
	offset := r.pos
	id, size := le.Uint16(r.data[offset:]), int(le.Uint16(r.data[offset+2:]))
	if size > 8224 || size > len(r.data)-offset-4 {
		return biffRecord{}, invalid("invalid BIFF record length")
	}
	r.pos += 4 + size
	return biffRecord{id, r.data[offset+4 : r.pos], offset}, nil
}

func requireBOF(r *biffReader, kind uint16) error {
	rec, err := r.next()
	if err != nil {
		return err
	}
	if rec.id != recordBOF || len(rec.data) < 4 {
		return invalid("missing BIFF8 BOF")
	}
	if le.Uint16(rec.data) != 0x0600 {
		return unsupported("only BIFF8 is supported")
	}
	if len(rec.data) != 16 {
		return invalid("invalid BIFF8 BOF length")
	}
	if le.Uint16(rec.data[2:]) != kind {
		return unsupported("unexpected BIFF substream type")
	}
	return nil
}

func readWorkbook(data []byte, sheetIndex int) ([][]string, error) {
	r := &biffReader{data: data}
	if err := requireBOF(r, 0x0005); err != nil {
		return nil, err
	}
	var offsets []int
	var strings []string
	hasSST := false
	for {
		rec, err := r.next()
		if err != nil {
			return nil, err
		}
		switch rec.id {
		case recordEOF:
			if len(rec.data) != 0 {
				return nil, invalid("nonempty EOF record")
			}
			return readSelectedSheet(data, r.pos, offsets, strings, sheetIndex)
		case 0x002F:
			return nil, unsupported("encrypted workbook")
		case recordBoundSheet:
			if len(rec.data) < 8 {
				return nil, invalid("truncated BOUNDSHEET")
			}
			if rec.data[5] != 0 {
				return nil, unsupported("only worksheet tabs are supported")
			}
			count, flags := int(rec.data[6]), rec.data[7]
			width := 1
			if flags&1 != 0 {
				width = 2
			}
			if count == 0 || count > 31 || flags&0xFE != 0 || len(rec.data) != 8+count*width {
				return nil, invalid("invalid sheet name")
			}
			if width == 2 {
				if _, err := decodeUTF16Bytes(rec.data[8:]); err != nil {
					return nil, err
				}
			}
			offset := le.Uint32(rec.data)
			if uint64(offset) >= uint64(len(data)) {
				return nil, invalid("sheet offset outside workbook")
			}
			offsets = append(offsets, int(offset))
		case recordSST:
			if hasSST {
				return nil, invalid("duplicate SST")
			}
			hasSST = true
			blocks := [][]byte{rec.data}
			for len(data)-r.pos >= 4 && le.Uint16(data[r.pos:]) == recordContinue {
				continuation, err := r.next()
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, continuation.data)
			}
			strings, err = readSST(blocks)
			if err != nil {
				return nil, err
			}
		case 0x0042: // CODEPAGE: BIFF8 uses Unicode.
			if len(rec.data) != 2 {
				return nil, invalid("invalid CODEPAGE")
			}
			// BIFF8 cell strings carry their own Unicode encoding flag.
		case 0x003D, 0x0022, 0x0031, 0x00E0, 0x0293, 0x0092, 0x01C1, 0x01AE, 0x0017:
			// WINDOW1, DATEMODE, FONT, XF, STYLE, PALETTE, RECALCID,
			// SUPBOOK and EXTERNSHEET do not affect stored text cell values.
		default:
			// Ignore records that do not contribute readable cell values.
		}
	}
}

func readSelectedSheet(data []byte, globalsEnd int, offsets []int, sst []string, index int) ([][]string, error) {
	if index < 0 || index >= len(offsets) {
		return nil, ErrSheetIndex
	}
	start, end := offsets[index], len(data)
	seen := make(map[int]bool)
	for _, offset := range offsets {
		if offset < globalsEnd || seen[offset] {
			return nil, invalid("invalid or duplicate sheet offset")
		}
		seen[offset] = true
		if offset > start && offset < end {
			end = offset
		}
	}
	r := &biffReader{data: data[start:end]}
	if err := requireBOF(r, 0x0010); err != nil {
		return nil, err
	}
	rows := make([][]string, 0)
	hasDimensions := false
	rowEnd, columnEnd := 0, 0
	pendingRow, pendingCol := -1, -1
	put := func(row, col int, value string) error {
		if !hasDimensions {
			return invalid("cell record before dimensions")
		}
		if row >= rowEnd || col >= columnEnd {
			return invalid("cell outside sheet dimensions")
		}
		if len(rows[row]) <= col {
			rows[row] = append(rows[row], make([]string, col+1-len(rows[row]))...)
		}
		rows[row][col] = value
		return nil
	}
	for {
		record, err := r.next()
		if err != nil {
			return nil, err
		}
		switch record.id {
		case recordEOF:
			if len(record.data) != 0 || !hasDimensions {
				return nil, invalid("missing dimensions or malformed sheet EOF")
			}
			return rows, nil
		case recordBOF:
			// Embedded chart substreams have their own EOF and must not end this sheet.
			depth := 1
			for depth > 0 {
				nested, err := r.next()
				if err != nil {
					return nil, err
				}
				if nested.id == recordBOF {
					depth++
				} else if nested.id == recordEOF {
					depth--
				}
			}
		case recordDimensions:
			if hasDimensions || len(record.data) != 14 {
				return nil, invalid("invalid or duplicate DIMENSIONS")
			}
			firstRow, lastRow := le.Uint32(record.data), le.Uint32(record.data[4:])
			firstCol, lastCol := le.Uint16(record.data[8:]), le.Uint16(record.data[10:])
			if firstRow > lastRow || lastRow > 65536 || firstCol > lastCol || lastCol > 256 {
				return nil, invalid("sheet dimensions exceed XLS limits")
			}
			rowEnd, columnEnd = int(lastRow), int(lastCol)
			rows = make([][]string, rowEnd)
			hasDimensions = true
		case recordMulRK, recordMulBlank:
			pendingRow, pendingCol = -1, -1
			stride := 6
			if record.id == recordMulBlank {
				stride = 2
			}
			if len(record.data) < 6+stride || (len(record.data)-6)%stride != 0 {
				return nil, invalid("invalid multiple-cell record")
			}
			row, first, last := int(le.Uint16(record.data)), int(le.Uint16(record.data[2:])), int(le.Uint16(record.data[len(record.data)-2:]))
			count := (len(record.data) - 6) / stride
			if last < first || last-first+1 != count {
				return nil, invalid("invalid multiple-cell column range")
			}
			for i := 0; i < count; i++ {
				value := ""
				if record.id == recordMulRK {
					value = numberString(rkNumber(le.Uint32(record.data[6+i*stride:])))
				}
				if err := put(row, first+i, value); err != nil {
					return nil, err
				}
			}
		case recordString:
			blocks, err := r.continuations(record.data)
			if err != nil {
				return nil, err
			}
			if pendingRow >= 0 {
				value, err := readSingleString(blocks)
				if err != nil {
					return nil, err
				}
				if err := put(pendingRow, pendingCol, value); err != nil {
					return nil, err
				}
				pendingRow, pendingCol = -1, -1
			}
		case recordLabelSST, recordBlank, recordNumber, recordRK, recordBoolErr, recordFormula, recordLabel, recordRString:
			pendingRow, pendingCol = -1, -1
			if len(record.data) < 6 {
				return nil, invalid("truncated cell record")
			}
			row, col := int(le.Uint16(record.data)), int(le.Uint16(record.data[2:]))
			value, needsString, err := cellValue(record, sst, r)
			if err != nil {
				return nil, err
			}
			if err := put(row, col, value); err != nil {
				return nil, err
			}
			if needsString {
				pendingRow, pendingCol = row, col
			}
		default:
			// Unknown records are ignored. Never guess row/column positions from an
			// unknown payload: it might be formatting, drawing, or formula metadata.
		}
	}
}

func (r *biffReader) continuations(first []byte) ([][]byte, error) {
	blocks := [][]byte{first}
	for len(r.data)-r.pos >= 4 && le.Uint16(r.data[r.pos:]) == recordContinue {
		record, err := r.next()
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, record.data)
	}
	return blocks, nil
}
