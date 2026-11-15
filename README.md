# xls-reader

A Go library for reading binary Excel XLS worksheets (BIFF8) as strings,
compatible with files created by [xls-writer](https://github.com/sergrom/xls-writer).
It uses only the Go standard library and has no dependency on the writer module.

## Usage

```go
package main

import (
    "fmt"
    "log"

    "github.com/sergrom/xls-reader"
)

func main() {
    rows, err := xlsreader.ReadXLS("cities.xls", 0)
    if err != nil {
        log.Fatal(err)
    }

    for _, row := range rows {
        fmt.Println(row)
    }
}
```

The second argument is the **zero-based sheet index**. For an existing reader:

```go
rows, err := xlsreader.Read(reader, 0)
```

`Read` does not close the supplied `io.Reader`. `ReadXLS` opens and closes its file.
Both return `[][]string` and an error. No partial rows are returned on failure.

## Supported files

- CFB/OLE version 3 containers with 512-byte sectors, including FAT, mini FAT,
  and DIFAT chains for larger files.
- BIFF8 workbook globals and worksheet records used by xls-writer.
- Multiple worksheets, shared and inline text, blank cells, and rich text
  (text is retained; formatting and phonetic metadata are ignored).
- Numbers (`NUMBER`, `RK`, `MULRK`), booleans, Excel errors, and cached formula
  results, including continued string results.
- Shared string tables, compressed Unicode, UTF-16, and strings spanning
  `CONTINUE` records, including supplementary characters such as emoji.
- Up to 65,536 rows and 256 columns per sheet, and 32,767 UTF-16 code units per cell.

Cell positions are preserved. Gaps before a stored cell become empty strings.
Each row ends at its last recognized cell, so rows can have different lengths.
Entirely empty rows are nil slices. Trailing empty rows are retained from the
worksheet's `DIMENSIONS` record. An empty sheet returns an empty slice.
Column widths do not add cells to the returned rows.

Only the selected worksheet's cell records are parsed. Workbook globals and
its shared string table are parsed for every read.

## Scope and errors

Values are converted without applying Excel display formats:

| Stored value | Returned string |
| --- | --- |
| Text | Original text. |
| Number | Decimal representation, e.g. `42`, `-12.5`, `0.0000001`. |
| Boolean | `TRUE` or `FALSE`. |
| Excel error | Its name, e.g. `#DIV/0!`, `#REF!`, `#N/A`. |
| Formula | Its saved result, converted using the same rules. |
| Missing, unrecognized, or non-finite value | Empty string. |

Formulas are **not evaluated**. A saved result can be stale; a formula without a
readable cached result remains blank. Numeric precision is limited to the value
stored in the XLS file. Dates and times are returned as their numeric Excel
serial values; currency, percentages, custom number formats, and leading zeros
from display formats are not reproduced. Textual leading zeros are preserved.

Unknown BIFF records are ignored without `ErrUnsupported`. The reader never
guesses cell coordinates from unknown record payloads: missing positions before
recognized cells remain empty. An unknown trailing cell encoding cannot extend
a row by itself. Recognized but unconverted cells (such as legacy `RSTRING`)
retain their position with an empty value. Embedded chart substreams are skipped.

This is not a complete Excel implementation. XLSX, older BIFF versions, CFB
version 4, encrypted workbooks, and non-worksheet tabs still return
`ErrUnsupported`. Corrupt or truncated recognized records still return
`ErrInvalidFile`; they are not silently treated as unsupported values.
Formatting, sheet names, document properties, and printing settings are not
exposed in the returned data.

Use `errors.Is` to distinguish:

| Error | Meaning |
| --- | --- |
| `ErrSheetIndex` | Negative or out-of-range sheet index. |
| `ErrInvalidFile` | Truncated or malformed structures, invalid offsets, cyclic chains, or invalid string references. |
| `ErrUnsupported` | Unsupported file format, encryption, or sheet kind (not cell value types). |
| `ErrLimit` | Input exceeds `MaxFileSize` (128 MiB). |

File-system and reader errors are wrapped, preserving `errors.Is`/`errors.As`.
Input bytes are buffered in memory; decoded strings and rows require additional
memory. The size limit bounds input buffering, not total memory consumption.

## Development

Go 1.20 or later is required.

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run='^$' -fuzz='^FuzzRead$' -fuzztime=15s
go test -run='^$' -fuzz='^FuzzBIFF$' -fuzztime=15s
```

Tests include writer/reader round trips, multiple sheets, empty and sparse rows,
Unicode continuations, numeric/boolean/error cells, cached formulas, rich text,
row/column limits, DIFAT files, ignored records,
truncated files, and broken sector chains. A snapshot of xls-writer is copied
into `internal/testwriter` solely for generating fixtures; production code does
not import it. Small hand-built BIFF fixtures test parsing independently of that
writer, including changes of encoding within a continued string. A checked-in
fixture generated by xlwt also tests values produced by an independent XLS writer;
Python is not required to run the Go tests.

Format references:

- [Microsoft CFB specification](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-cfb/53989ce4-7b05-4f8d-829b-d08d6148375b)
- [Microsoft BIFF8 shared strings](https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-xls/b6231b92-d32e-4626-badd-c3310a672bab)

## License

[MIT](LICENSE).
