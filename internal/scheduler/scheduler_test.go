package scheduler

import "testing"

func TestRemainingCalendarDays(t *testing.T) {
	got, err := remaining("2026-11-22", "2026-11-12")
	if err != nil || got != 10 {
		t.Fatalf("remaining() = %d, %v; want 10", got, err)
	}
}

func TestRenderTemplateVariables(t *testing.T) {
	x := strategy{
		Template:      "{{project}} {{customer}} {{vendor}} {{model}} {{sn}} {{end}} {{days}} {{owner}}",
		TitleTemplate: "剩余 {{days}} 天",
		Format:        "markdown",
	}
	d := device{Project: "项目A", Customer: "客户B", Vendor: "厂商", Model: "型号", SN: "SN-1", End: "2026-12-01", Owner: "负责人"}
	title, got := renderOne(x, "2026-11", d.asTemplateDevice(), 19)
	want := "项目A 客户B 厂商 型号 SN-1 2026-12-01 19 负责人"
	if got != want {
		t.Fatalf("renderOne() body = %q; want %q", got, want)
	}
	if title != "剩余 19 天" {
		t.Fatalf("renderOne() title = %q; want %q", title, "剩余 19 天")
	}
}
