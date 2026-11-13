package importexport

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

type Row struct {
	Number int
	Values map[string]string
}
type RowError struct {
	Number  int    `json:"row"`
	Message string `json:"message"`
}
type Result struct {
	Imported int        `json:"imported"`
	Errors   []RowError `json:"errors"`
}

var DeviceColumns = []string{"项目", "厂商", "型号", "SN", "维保开始", "维保结束", "状态", "负责人", "合同号", "备注"}
var ProjectColumns = []string{"项目名", "客户", "联系人", "合同号", "备注"}

func ReadXLSX(r io.Reader, required []string) ([]Row, error) {
	book, e := excelize.OpenReader(r)
	if e != nil {
		return nil, e
	}
	defer book.Close()
	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("Excel 文件没有工作表")
	}
	sheet := sheets[0]
	records, e := book.GetRows(sheet)
	if e != nil {
		return nil, e
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("Excel 文件为空")
	}
	// Date cells typed in Excel (e.g. 2026/6/11) return their raw serial
	// number via GetRows; convert them to YYYY-MM-DD so validation accepts
	// them like text dates.
	cellDate := func(row, col int) (string, bool) {
		cell, err := excelize.CoordinatesToCellName(col+1, row)
		if err != nil {
			return "", false
		}
		raw, err := book.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
		if err != nil || raw == "" {
			return "", false
		}
		serial, err := strconv.ParseFloat(raw, 64)
		if err != nil || serial <= 0 {
			return "", false
		}
		parsed, err := excelize.ExcelDateToTime(serial, false)
		if err != nil {
			return "", false
		}
		return parsed.Format("2006-01-02"), true
	}
	// Locate the header row within the first few rows (project-prefilled
	// templates put metadata above the header).
	index := map[string]int{}
	headerRow := -1
	for i, record := range records {
		if i >= 10 {
			break
		}
		found := map[string]int{}
		for col, v := range record {
			found[strings.TrimSpace(v)] = col
		}
		missing := false
		for _, name := range required {
			if _, ok := found[name]; !ok {
				missing = true
				break
			}
		}
		if !missing {
			index, headerRow = found, i
			break
		}
	}
	if headerRow == -1 {
		return nil, fmt.Errorf("缺少必需列：%s", strings.Join(required, "、"))
	}
	out := []Row{}
	for i, record := range records[headerRow+1:] {
		values := map[string]string{}
		for name, col := range index {
			if col < len(record) {
				values[name] = strings.TrimSpace(record[col])
			}
		}
		// Replace date-serial values for the known date columns with
		// formatted dates; empty cells are left alone. Data index i sits
		// at Excel row i+headerRow+2 (1-based, header included).
		for _, dateColumn := range []string{"维保开始", "维保结束"} {
			if col, ok := index[dateColumn]; ok && values[dateColumn] != "" {
				if formatted, isDate := cellDate(i+headerRow+2, col); isDate {
					values[dateColumn] = formatted
				}
			}
		}
		out = append(out, Row{Number: i + headerRow + 2, Values: values})
	}
	return out, nil
}

func WriteTemplate(w io.Writer, columns []string) error {
	f := excelize.NewFile()
	defer f.Close()
	for i, v := range columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if e := f.SetCellValue("Sheet1", cell, v); e != nil {
			return e
		}
	}
	return f.Write(w)
}
