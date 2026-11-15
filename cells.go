package xlsreader

import (
	"math"
	"strconv"
)

const (
	recordNumber   = 0x0203
	recordRK       = 0x027E
	recordMulRK    = 0x00BD
	recordMulBlank = 0x00BE
	recordBoolErr  = 0x0205
	recordFormula  = 0x0006
	recordString   = 0x0207
	recordLabel    = 0x0204
	recordRString  = 0x00D6
)

func numberString(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func rkNumber(value uint32) float64 {
	var result float64
	if value&2 != 0 {
		result = float64(int32(value) >> 2)
	} else {
		result = math.Float64frombits(uint64(value&^3) << 32)
	}
	if value&1 != 0 {
		result /= 100
	}
	return result
}

func booleanString(value byte) string {
	switch value {
	case 0:
		return "FALSE"
	case 1:
		return "TRUE"
	default:
		return ""
	}
}

func errorString(value byte) string {
	switch value {
	case 0x00:
		return "#NULL!"
	case 0x07:
		return "#DIV/0!"
	case 0x0F:
		return "#VALUE!"
	case 0x17:
		return "#REF!"
	case 0x1D:
		return "#NAME?"
	case 0x24:
		return "#NUM!"
	case 0x2A:
		return "#N/A"
	case 0x2B:
		return "#GETTING_DATA"
	default:
		return ""
	}
}

// cellValue reads only stored values, never formula expressions or formats.
// needsString indicates that a formula's cached text is in a following STRING.
func cellValue(record biffRecord, sst []string, r *biffReader) (value string, needsString bool, err error) {
	data := record.data
	switch record.id {
	case recordBlank:
		if len(data) != 6 {
			return "", false, invalid("invalid BLANK length")
		}
	case recordLabelSST:
		if len(data) != 10 {
			return "", false, invalid("invalid LABELSST length")
		}
		id := le.Uint32(data[6:])
		if uint64(id) >= uint64(len(sst)) {
			return "", false, invalid("cell references missing SST string")
		}
		return sst[id], false, nil
	case recordNumber:
		if len(data) != 14 {
			return "", false, invalid("invalid NUMBER length")
		}
		return numberString(math.Float64frombits(le.Uint64(data[6:]))), false, nil
	case recordRK:
		if len(data) != 10 {
			return "", false, invalid("invalid RK length")
		}
		return numberString(rkNumber(le.Uint32(data[6:]))), false, nil
	case recordBoolErr:
		if len(data) != 8 {
			return "", false, invalid("invalid BOOLERR length")
		}
		if data[7] == 0 {
			return booleanString(data[6]), false, nil
		}
		if data[7] == 1 {
			return errorString(data[6]), false, nil
		}
	case recordFormula:
		if len(data) < 22 || int(le.Uint16(data[20:])) > len(data)-22 {
			return "", false, invalid("truncated FORMULA")
		}
		result := data[6:14]
		if le.Uint16(result[6:]) != 0xFFFF {
			return numberString(math.Float64frombits(le.Uint64(result))), false, nil
		}
		switch result[0] {
		case 0:
			return "", true, nil
		case 1:
			return booleanString(result[2]), false, nil
		case 2:
			return errorString(result[2]), false, nil
			// Blank and unrecognized cached result types remain empty.
		}
	case recordLabel:
		blocks, err := r.continuations(data[6:])
		if err != nil {
			return "", false, err
		}
		value, err := readSingleString(blocks)
		return value, false, err
	case recordRString:
		// Older rich-label cell encoding: retain its position, leave its value blank.
	}
	return "", false, nil
}
