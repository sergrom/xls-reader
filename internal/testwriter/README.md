# Test fixture writer

Source copied from the owner's `github.com/sergrom/xls-writer` project on
2026-11-15, with its MIT license. Used only by the reader's tests to generate
real XLS fixtures without a dependency on the external module.

The production reader does not import this package. Changes to this copy should
be deliberate: keeping it independent helps detect reader/writer regressions.
