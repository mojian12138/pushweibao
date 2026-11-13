package importexport

import (
	"bytes"
	"io"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestTemplateRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTemplate(&buf, DeviceColumns); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadXLSX(bytes.NewReader(buf.Bytes()), DeviceColumns[:6])
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("template should have no data rows, got %d", len(rows))
	}
	if _, err := ReadXLSX(io.LimitReader(bytes.NewReader([]byte("not xlsx")), 9), DeviceColumns); err == nil {
		t.Fatal("expected invalid workbook error")
	}
}

var projectDeviceColumns = []string{"厂商", "型号", "SN", "维保开始", "维保结束", "状态", "负责人", "备注"}

func TestReadXLSXLocatesPrefilledHeader(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	// Row 1 metadata, row 2 blank, row 3 header, rows 4-5 data — the
	// project-device template layout.
	for i, v := range []string{"项目名称", "项目01", "合同号", "10000001"} {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue("Sheet1", cell, v)
	}
	for i, column := range projectDeviceColumns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 3)
		_ = f.SetCellValue("Sheet1", cell, column)
	}
	_ = f.SetCellValue("Sheet1", "A4", "H3C")
	_ = f.SetCellValue("Sheet1", "B4", "WA7220H-EI")
	_ = f.SetCellValue("Sheet1", "C4", "SN-1")
	_ = f.SetCellValue("Sheet1", "D4", "2026/6/11")
	_ = f.SetCellValue("Sheet1", "E4", "2027/9/9")
	_ = f.SetCellValue("Sheet1", "A5", "H3C")
	_ = f.SetCellValue("Sheet1", "B5", "WA7220H-EI")
	_ = f.SetCellValue("Sheet1", "C5", "SN-2")
	_ = f.SetCellValue("Sheet1", "D5", "2026-06-11")
	_ = f.SetCellValue("Sheet1", "E5", "2027-09-09")
	// Row 6: date-typed cells, as Excel produces when the operator types
	// 2026/6/11 and the cell auto-formats.
	_ = f.SetCellValue("Sheet1", "A6", "H3C")
	_ = f.SetCellValue("Sheet1", "B6", "WA7220H-EI")
	_ = f.SetCellValue("Sheet1", "C6", "SN-3")
	_ = f.SetCellDefault("Sheet1", "D6", "46184") // 2026-06-11
	_ = f.SetCellDefault("Sheet1", "E6", "46565") // 2027-06-27
	_, _ = f.NewStyle(&excelize.Style{NumFmt: 14, CustomNumFmt: nil})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadXLSX(bytes.NewReader(buf.Bytes()), projectDeviceColumns[:5])
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 data rows, got %d", len(rows))
	}
	if rows[0].Number != 4 || rows[0].Values["SN"] != "SN-1" || rows[1].Number != 5 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if rows[0].Values["维保开始"] != "2026/6/11" {
		t.Fatalf("date not preserved for parsing: %q", rows[0].Values["维保开始"])
	}
	if got := rows[2].Values["维保开始"]; got != "2026-06-11" {
		t.Fatalf("date-typed cell not converted: %q", got)
	}
	if got := rows[2].Values["维保结束"]; got != "2027-06-27" {
		t.Fatalf("end date-typed cell not converted: %q", got)
	}
}
