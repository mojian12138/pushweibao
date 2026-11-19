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
			// A title is a single short line, so it is checked against the
			// markers a title accepts; the milestone title may name the
			// number of devices the message covers.
			var err error
			if strings.HasSuffix(name, "_title") {
				err = Validate(kind, text, true)
			} else {
				err = Validate(kind, text, false)
			}
			if err != nil {
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

// TestDefaultBodyForFallsBackToMarkdown checks that an unknown format still
// gives a usable template. Markdown is the default of the program, so an
// unknown value is treated as markdown instead of html.
func TestDefaultBodyForFallsBackToMarkdown(t *testing.T) {
	if DefaultBodyFor("nope", KindMilestone) != DefaultBodyFor(FormatMarkdown, KindMilestone) {
		t.Error("expected an unknown format to fall back to markdown")
	}
}

// TestNormalizeFormat checks the cleanup of a format. Markdown is the
// default, because its messages render best in PushPlus.
func TestNormalizeFormat(t *testing.T) {
	if NormalizeFormat("") != FormatMarkdown {
		t.Errorf("expected an empty format to fall back to markdown, got %q", NormalizeFormat(""))
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
