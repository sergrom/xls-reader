# Independent XLS fixture

`mixed-xlwt.xls` was generated with xlwt 1.3.0. It contains plain/rich text,
integers, decimals, booleans, a date, and a formula whose cached result is blank.
The fixture is read by Go tests; xlwt is not a project dependency.

The generation script is `generate_fixture.py` (requires xlwt 1.3.0).
