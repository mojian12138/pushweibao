package template

import (
	"strings"
	"testing"
)

// TestPresetsAreValidTemplates checks that every ready-made template of
// every format can be used as it is. A preset that the validator would
// refuse would be offered in the editor and then rejected on save.
func TestPresetsAreValidTemplates(t *testing.T) {
	for _, format := range Formats {
		for name, text := range Presets(format) {
			kind := KindMilestone
			if strings.HasPrefix(name, "monthly_body") || strings.HasPrefix(name, "monthly_title") {
				kind = KindMonthly
			} else if strings.HasPrefix(name, "cycle") {
				kind = KindCycle
			}
			// The item of the monthly summary is one line of a device, so
			// it uses the device markers.
			if err := Validate(kind, text, false); err != nil {
				t.Errorf("the preset %s of the format %s is not a valid template: %v", name, format, err)
			}
		}
	}
}

// TestEveryFormatHasItsOwnPreset checks that the three formats do not share
// the same text, so that choosing a format really changes the message.
func TestEveryFormatHasItsOwnPreset(t *testing.T) {
	seen := map[string]string{}
	for _, format := range Formats {
		body := DefaultBodyFor(format, KindMilestone)
		if other, ok := seen[body]; ok {
			t.Errorf("the formats %s and %s share the same body", other, format)
		}
		seen[body] = format
	}
}

// TestDefaultTitleForPicksTheMonthlyTitle checks that the monthly summary
// gets its own title and the other kinds the reminder one.
func TestDefaultTitleForPicksTheMonthlyTitle(t *testing.T) {
	monthly := DefaultTitleFor(FormatTxt, KindMonthly)
	reminder := DefaultTitleFor(FormatTxt, KindMilestone)
	if monthly == "" || reminder == "" {
		t.Fatal("expected a title for every kind")
	}
	if monthly == reminder {
		t.Error("expected the monthly summary to have its own title")
	}
	if !contains(monthly, "{{month}}") {
		t.Errorf("expected the monthly title to use the month, got %q", monthly)
	}
}

// TestDefaultBodyForFallsBackToHTML checks that an unknown format still
// gives a usable template.
func TestDefaultBodyForFallsBackToHTML(t *testing.T) {
	if DefaultBodyFor("nope", KindMilestone) != DefaultBodyFor(FormatHTML, KindMilestone) {
		t.Error("expected an unknown format to fall back to html")
	}
}

// TestNormalizeFormat checks the cleanup of a format.
func TestNormalizeFormat(t *testing.T) {
	if NormalizeFormat("") != FormatHTML {
		t.Errorf("expected an empty format to fall back to html, got %q", NormalizeFormat(""))
	}
	if NormalizeFormat(" TXT ") != FormatTxt {
		t.Errorf("expected the format to be cleaned up, got %q", NormalizeFormat(" TXT "))
	}
	if !ValidFormat(FormatMarkdown) {
		t.Error("expected markdown to be a valid format")
	}
	if ValidFormat("nope") {
		t.Error("expected an unknown format to be refused")
	}
}
