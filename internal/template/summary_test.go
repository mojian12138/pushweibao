package template

import (
	"strings"
	"testing"
)

func d(project, sn, end string) Device {
	return Device{Project: project, Vendor: "厂商", Model: "型号", SN: sn, End: end, Owner: "李四"}
}

// TestBuildMilestoneItemsGroupsByTier checks that the devices of one day
// are grouped by the tier they fired on, the tiers in urgency order and
// the devices inside by the days that are left.
func TestBuildMilestoneItemsGroupsByTier(t *testing.T) {
	hits := []MilestoneHit{
		{Device: d("项目A", "SN-A", "2026-11-19"), Days: 0, Offset: 0},
		{Device: d("项目B", "SN-B", "2026-11-26"), Days: 7, Offset: 7},
		{Device: d("项目C", "SN-C", "2026-12-19"), Days: 30, Offset: 30},
		{Device: d("项目D", "SN-D", "2026-12-19"), Days: 30, Offset: 30},
	}
	got := BuildMilestoneItems(FormatMarkdown, DefaultOffsets, hits)
	if !strings.Contains(got, "### 🔴 今日到期 · 1 台") {
		t.Errorf("the tier of today has no heading: %q", got)
	}
	seven := strings.Index(got, "### ⏰ 剩余 ≤7 天 · 1 台")
	thirty := strings.Index(got, "### ⏰ 剩余 ≤30 天 · 2 台")
	if seven < 0 || thirty < 0 || seven > thirty {
		t.Errorf("the tiers are not in urgency order: %q", got)
	}
	if strings.Index(got, "SN-B") > strings.Index(got, "SN-C") {
		t.Errorf("the 7 day device should sit above the 30 day ones: %q", got)
	}
}

// TestBuildMilestoneItemsSortsInsideTheTier checks that two devices of the
// same tier are ordered by the days that are left, so the most urgent one
// is read first.
func TestBuildMilestoneItemsSortsInsideTheTier(t *testing.T) {
	hits := []MilestoneHit{
		{Device: d("项目B", "SN-LATE", "2026-12-19"), Days: 30, Offset: 30},
		{Device: d("项目A", "SN-SOON", "2026-12-17"), Days: 28, Offset: 30},
	}
	got := BuildMilestoneItems(FormatMarkdown, DefaultOffsets, hits)
	if strings.Index(got, "SN-SOON") > strings.Index(got, "SN-LATE") {
		t.Errorf("the devices are not sorted by urgency: %q", got)
	}
}

// TestBuildSummaryBlocksSplitsAndOrders checks that the summary carries one
// block for the devices that are about to expire and one for the ones that
// already have, each headed and ordered by urgency.
func TestBuildSummaryBlocksSplitsAndOrders(t *testing.T) {
	rows := []SummaryRow{
		{Device: d("项目A", "SN-A", "2027-01-03"), Days: 45},
		{Device: d("项目B", "SN-B", "2026-11-29"), Days: 10},
		{Device: d("项目C", "SN-C", "2026-11-19"), Days: 0},
		{Device: d("项目D", "SN-D", "2026-11-14"), Days: -5},
		{Device: d("项目E", "SN-E", "2026-10-20"), Days: -30},
	}
	upcoming, expired := BuildSummaryBlocks(FormatMarkdown, DefaultItemFor(FormatMarkdown), rows)
	if !strings.Contains(upcoming, "### 🔔 即将到期 · 3 台") {
		t.Errorf("the upcoming block lost its heading: %q", upcoming)
	}
	if !strings.Contains(expired, "### ⛔ 已过期 · 2 台") {
		t.Errorf("the expired block lost its heading: %q", expired)
	}
	if strings.Index(upcoming, "SN-C") > strings.Index(upcoming, "SN-B") {
		t.Errorf("the upcoming devices are not in urgency order: %q", upcoming)
	}
	if strings.Index(expired, "SN-D") > strings.Index(expired, "SN-E") {
		t.Errorf("the expired devices are not in urgency order: %q", expired)
	}
	// The blank line between the heading and the table keeps the table from
	// being read as part of the heading paragraph, which is what folded the
	// summary of the earlier release into one line.
	if !strings.Contains(upcoming, "· 3 台\n\n| 项目 | 型号 |") {
		t.Errorf("the heading and the table are not apart: %q", upcoming)
	}
}

// TestBuildSummaryLinesLeavesTheHeadingsOut checks that the plain lines fit
// the {{items}} and {{expired}} markers of a hand-written template, without
// the headings of the blocks.
func TestBuildSummaryLinesLeavesTheHeadingsOut(t *testing.T) {
	rows := []SummaryRow{
		{Device: d("项目A", "SN-A", "2026-11-19"), Days: 0},
		{Device: d("项目B", "SN-B", "2026-11-14"), Days: -5},
	}
	upcoming, expired := BuildSummaryLines(DefaultItemFor(FormatMarkdown), rows)
	if strings.Contains(upcoming, "🔔") || strings.Contains(expired, "⛔") {
		t.Errorf("the plain lines carry a heading: %q %q", upcoming, expired)
	}
	if !strings.Contains(upcoming, "SN-A") || !strings.Contains(expired, "SN-B") {
		t.Errorf("the devices went into the wrong block: %q %q", upcoming, expired)
	}
}

// TestBuildSummaryBlocksWithoutDevices checks that an empty summary builds
// two empty blocks, so that the message carries no empty heading.
func TestBuildSummaryBlocksWithoutDevices(t *testing.T) {
	upcoming, expired := BuildSummaryBlocks(FormatMarkdown, "", nil)
	if upcoming != "" || expired != "" {
		t.Errorf("expected empty blocks, got %q and %q", upcoming, expired)
	}
}

// TestMilestoneItemsForOneDevice checks that the list of a single-device
// message still shows the grouped layout, so that the preview matches the
// real message.
func TestMilestoneItemsForOneDevice(t *testing.T) {
	got := MilestoneItemsForOne(FormatMarkdown, d("项目A", "SN-A", "2026-12-19"), 30)
	if !strings.Contains(got, "剩余 ≤30 天 · 1 台") {
		t.Errorf("the single device lost its tier heading: %q", got)
	}
	if !strings.Contains(got, "SN-A") {
		t.Errorf("the device is missing: %q", got)
	}
}
