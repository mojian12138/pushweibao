package template

import "testing"

func TestValidateAcceptsKnownMarkers(t *testing.T) {
	if err := Validate(KindMilestone, "项目：{{project}} 剩余 {{days}} 天", false); err != nil {
		t.Errorf("expected the template to be accepted, got %v", err)
	}
	if err := Validate(KindMonthly, "{{month}} 共 {{count}} 台", false); err != nil {
		t.Errorf("expected the monthly template to be accepted, got %v", err)
	}
	// A title accepts the summary markers too.
	if err := Validate(KindMonthly, "{{month}} 汇总 {{count}}", true); err != nil {
		t.Errorf("expected the title to be accepted, got %v", err)
	}
}

func TestValidateRejectsUnknownMarkers(t *testing.T) {
	// A device marker is not available in a monthly summary.
	err := Validate(KindMonthly, "项目：{{project}}", false)
	if err == nil {
		t.Fatal("expected an unknown marker to be rejected")
	}
	if !contains(err.Error(), "project") {
		t.Errorf("expected the offending marker in the message, got %v", err)
	}
}

func TestValidateRejectsCrossTypeMarkers(t *testing.T) {
	if err := Validate(KindMilestone, "{{month}}", false); err == nil {
		t.Error("expected the monthly marker to be rejected for a device reminder")
	}
	if err := Validate(KindMonthly, "{{sn}}", false); err == nil {
		t.Error("expected the device marker to be rejected for a monthly summary")
	}
}

func TestValidateAllowsAnEmptyTemplate(t *testing.T) {
	if err := Validate(KindMilestone, "", false); err != nil {
		t.Errorf("expected an empty template to be accepted, got %v", err)
	}
}

func TestRenderReplacesMarkers(t *testing.T) {
	d := Device{Project: "某项目", Customer: "某客户", Vendor: "厂商", Model: "XC-200", SN: "SN1", End: "2026-12-31", Owner: "张三"}
	got := Render("项目：{{project}} 型号：{{model}} 剩余 {{days}} 天", d, 30)
	want := "项目：某项目 型号：XC-200 剩余 30 天"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestRenderKeepsUnknownMarkersVisible(t *testing.T) {
	// An unknown marker stays in the text so that a mistake is visible in
	// the preview instead of turning into a blank message.
	got := Render("项目：{{project}} 错误：{{nope}}", Device{Project: "某项目"}, 10)
	if got != "项目：某项目 错误：{{nope}}" {
		t.Errorf("expected the unknown marker to be kept, got %q", got)
	}
}

func TestRenderTitleSupportsBothMarkerSets(t *testing.T) {
	d := Device{Project: "某项目", Vendor: "厂商", Model: "XC-200"}
	got := RenderTitle("{{project}}｜{{device}}", d, 5)
	if got != "某项目｜厂商 XC-200" {
		t.Errorf("expected the device title, got %q", got)
	}
	got = RenderTitleSummary("{{month}} 汇总", Summary{Month: "2026-11"})
	if got != "2026-11 汇总" {
		t.Errorf("expected the summary title, got %q", got)
	}
}

func TestSampleFillsEveryMarker(t *testing.T) {
	got := Sample(KindMilestone, "{{project}} 剩余 {{days}} 天")
	if got == "项目：{{project}}" || contains(got, "{{") {
		t.Errorf("expected the sample to replace every marker, got %q", got)
	}
}

func TestUnknownListsEachMarkerOnce(t *testing.T) {
	got := Unknown(KindMonthly, "{{project}} {{project}} {{sn}}", false)
	if len(got) != 2 {
		t.Errorf("expected 2 unknown markers, got %v", got)
	}
	if got[0] != "project" || got[1] != "sn" {
		t.Errorf("expected the markers to be sorted, got %v", got)
	}
}

func TestMarkersKeepsTheOrder(t *testing.T) {
	got := Markers("{{project}} {{days}} {{project}}")
	if len(got) != 2 || got[0] != "project" || got[1] != "days" {
		t.Errorf("expected the markers in order, got %v", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
