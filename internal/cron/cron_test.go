package cron

import (
	"testing"
	"time"
)

func zone() *time.Location { return time.FixedZone("CST", 8*60*60) }

func mustParse(t *testing.T, expr string) *Schedule {
	t.Helper()
	s, err := Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return s
}

// TestParseAcceptsTheCommonForms checks the syntaxes a reminder schedule
// realistically uses.
func TestParseAcceptsTheCommonForms(t *testing.T) {
	for _, expr := range []string{
		"* * * * *", "0 9 * * *", "30 8 1 * *", "0 9 * * 1",
		"*/5 * * * *", "0 9-18 * * *", "0 9,18 * * *", "1-15/2 8 * * *",
		"0 9 * jan-mar *", "0 9 * * mon-fri", "0 0 1 1 *", "@monthly",
		"@daily", "@weekly", "@yearly", "@hourly", "0 9 * * 7",
	} {
		if _, err := Parse(expr); err != nil {
			t.Errorf("parse %q: %v", expr, err)
		}
	}
}

// TestParseRefusesTheBrokenForms checks that a broken expression is refused
// instead of silently matching another time.
func TestParseRefusesTheBrokenForms(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "* * * * * *", "61 * * * *", "0 25 * * *", "0 9 * * 8", "*/0 * * * *", "5-2 8 * * *", "0 9 * 13 *"} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("parse %q should have failed", expr)
		}
	}
}

// TestMatches checks the matching of a few schedules against known times.
func TestMatches(t *testing.T) {
	// 2026-11-19 is a Thursday.
	t.Run("每月1日9点", func(t *testing.T) {
		s := mustParse(t, "0 9 1 * *")
		hit := time.Date(2026, 12, 1, 9, 0, 0, 0, zone())
		miss := time.Date(2026, 12, 1, 9, 1, 0, 0, zone())
		if !s.Matches(hit) || s.Matches(miss) {
			t.Error("the schedule matched the wrong times")
		}
	})
	t.Run("工作日9点", func(t *testing.T) {
		s := mustParse(t, "0 9 * * 1-5")
		monday := time.Date(2026, 11, 23, 9, 0, 0, 0, zone())
		saturday := time.Date(2026, 11, 21, 9, 0, 0, 0, zone())
		if !s.Matches(monday) || s.Matches(saturday) {
			t.Error("the weekday schedule matched the wrong times")
		}
	})
	t.Run("每5分钟", func(t *testing.T) {
		s := mustParse(t, "*/5 * * * *")
		if !s.Matches(time.Date(2026, 11, 19, 14, 15, 0, 0, zone())) {
			t.Error("the minute step did not match 14:15")
		}
		if s.Matches(time.Date(2026, 11, 19, 14, 16, 0, 0, zone())) {
			t.Error("the minute step matched 14:16")
		}
	})
	t.Run("周日两种写法", func(t *testing.T) {
		s := mustParse(t, "0 9 * * sun")
		s7 := mustParse(t, "0 9 * * 7")
		sunday := time.Date(2026, 11, 22, 9, 0, 0, 0, zone())
		if !s.Matches(sunday) || !s7.Matches(sunday) {
			t.Error("the sunday spellings did not both match")
		}
	})
}

// TestNextStepsForward checks that Next finds the next matching time of a
// few schedules.
func TestNextStepsForward(t *testing.T) {
	t.Run("每月1日", func(t *testing.T) {
		s := mustParse(t, "0 9 1 * *")
		from := time.Date(2026, 11, 19, 14, 0, 0, 0, zone())
		next := s.Next(from)
		want := time.Date(2026, 12, 1, 9, 0, 0, 0, zone())
		if !next.Equal(want) {
			t.Errorf("next = %s, want %s", next, want)
		}
	})
	t.Run("每天9点跨年", func(t *testing.T) {
		s := mustParse(t, "0 9 * * *")
		from := time.Date(2026, 12, 31, 10, 0, 0, 0, zone())
		next := s.Next(from)
		want := time.Date(2027, 1, 1, 9, 0, 0, 0, zone())
		if !next.Equal(want) {
			t.Errorf("next = %s, want %s", next, want)
		}
	})
	t.Run("每5分钟", func(t *testing.T) {
		s := mustParse(t, "*/5 * * * *")
		from := time.Date(2026, 11, 19, 14, 2, 0, 0, zone())
		next := s.Next(from)
		want := time.Date(2026, 11, 19, 14, 5, 0, 0, zone())
		if !next.Equal(want) {
			t.Errorf("next = %s, want %s", next, want)
		}
	})
}

// TestDescribeNamesTheSchedule checks the plain words for the common
// schedules.
func TestDescribeNamesTheSchedule(t *testing.T) {
	if got := Describe("0 9 1 * *"); got != "每月 1 日 09:00" {
		t.Errorf("describe = %q", got)
	}
	if got := Describe("@daily"); got != "每天 00:00" {
		t.Errorf("describe = %q", got)
	}
	if got := Describe("*/5 * * * *"); got != "*/5 * * * *" {
		t.Errorf("describe = %q", got)
	}
}
