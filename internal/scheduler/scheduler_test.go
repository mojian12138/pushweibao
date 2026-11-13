package scheduler

import "testing"

func TestRemainingCalendarDays(t *testing.T) {
	got, err := remaining("2026-11-22", "2026-11-12")
	if err != nil || got != 10 {
		t.Fatalf("remaining() = %d, %v; want 10", got, err)
	}
}

func TestRenderTemplateVariables(t *testing.T) {
	d := device{Project: "项目A", Customer: "客户B", Vendor: "厂商", Model: "型号", SN: "SN-1", End: "2026-12-01", Owner: "负责人"}
	got := render("{{project}} {{customer}} {{vendor}} {{model}} {{sn}} {{end}} {{days}} {{owner}}", d, 19)
	want := "项目A 客户B 厂商 型号 SN-1 2026-12-01 19 负责人"
	if got != want {
		t.Fatalf("render() = %q; want %q", got, want)
	}
}
