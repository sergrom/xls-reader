"""Regenerate mixed-xlwt.xls with xlwt==1.3.0 (test fixture only)."""
import datetime
from pathlib import Path

import xlwt

workbook = xlwt.Workbook()
sheet = workbook.add_sheet("Mixed")
for column, value in enumerate(["City", 42, -12.5, True, False, "001", "🐧"]):
    sheet.write(0, column, value)
sheet.write(1, 0, 123.45)
sheet.write(1, 1, 1.23456789012345)
sheet.write(1, 2, datetime.datetime(2024, 1, 1), xlwt.easyxf(num_format_str="YYYY-MM-DD"))
sheet.write(1, 3, xlwt.Formula("1+2"))
sheet.write_rich_text(1, 4, ["rich ", ("text", xlwt.easyfont("bold on"))])
sheet.write(1, 5, "after formula")
workbook.save(str(Path(__file__).with_name("mixed-xlwt.xls")))
