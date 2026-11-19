package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A small cron parser for the trigger schedules of the notification
// strategies. It supports the five field form — minute hour day month
// weekday — with the usual syntax: `*`, a single value, a list, a step and
// a range, combined as in `*/5`, `1-15/2`, `1,15 3 * * 1`. The names of
// the months and the weekdays are accepted in English, three letters or
// full. The macros `@yearly`, `@monthly`, `@weekly`, `@daily` and
// `@hourly` are written out, as is `@every <duration>` with a duration in
// hours or minutes.
//
// The parser does not aim at the whole of the grammar a server cron
// accepts; it covers what a reminder schedule realistically uses.

// fieldSet is the set of values one field of an expression accepts.
type fieldSet struct {
	min, max int
	values   map[int]bool
}

// has reports whether the field matches the value given.
func (f fieldSet) has(v int) bool { return f.values[v] }

// names maps the english names of a field to their numbers, for the month
// and the weekday.
var monthNames = map[string]int{
	"jan": 1, "january": 1, "feb": 2, "february": 2, "mar": 3, "march": 3,
	"apr": 4, "april": 4, "may": 5, "jun": 6, "june": 6, "jul": 7, "july": 7,
	"aug": 8, "august": 8, "sep": 9, "september": 9, "oct": 10, "october": 10,
	"nov": 11, "november": 11, "dec": 12, "december": 12,
}

// The weekday of Go starts on Sunday with 0, the weekday of cron starts on
// Sunday with 0 too, but 7 is written for it as well.
var weekdayNames = map[string]int{
	"sun": 0, "sunday": 0, "mon": 1, "monday": 1, "tue": 2, "tuesday": 2,
	"wed": 3, "wednesday": 3, "thu": 4, "thursday": 4, "fri": 5, "friday": 5,
	"sat": 6, "saturday": 6,
}

// parseField reads one field of an expression into its set of values.
func parseField(field string, min, max int, names map[string]int) (fieldSet, error) {
	out := fieldSet{min: min, max: max, values: map[int]bool{}}
	if field == "*" {
		for v := min; v <= max; v++ {
			out.values[v] = true
		}
		return out, nil
	}
	for _, part := range strings.Split(field, ",") {
		step := 1
		if at := strings.Index(part, "/"); at >= 0 {
			n, err := strconv.Atoi(part[at+1:])
			if err != nil || n <= 0 {
				return out, fmt.Errorf("步长无效：%s", part)
			}
			step = n
			part = part[:at]
		}
		lo, hi := min, max
		switch {
		case part == "*":
			// The range stays the whole field.
		case strings.Contains(part, "-"):
			bounds := strings.SplitN(part, "-", 2)
			var err error
			lo, err = parseValue(bounds[0], names)
			if err != nil {
				return out, err
			}
			hi, err = parseValue(bounds[1], names)
			if err != nil {
				return out, err
			}
			if lo > hi || lo < min || hi > max {
				return out, fmt.Errorf("范围无效：%s", part)
			}
		default:
			v, err := parseValue(part, names)
			if err != nil {
				return out, err
			}
			if v < min || v > max {
				return out, fmt.Errorf("数值超出范围：%s", part)
			}
			lo, hi = v, v
		}
		if step > 1 && part == "*" {
			lo = min
		}
		for v := lo; v <= hi; v += step {
			out.values[v] = true
		}
	}
	if len(out.values) == 0 {
		return out, fmt.Errorf("字段为空")
	}
	return out, nil
}

// parseValue reads one value of a field, a number or a name.
func parseValue(text string, names map[string]int) (int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if names != nil {
		if v, ok := names[text]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("无法识别的值：%s", text)
	}
	return v, nil
}

// Schedule is a parsed cron expression. It knows the fields it matches and
// the zone the times are read in.
type Schedule struct {
	minute, hour, day, month, weekday fieldSet
}

// Parse reads a cron expression. The expression may carry seconds in a
// sixth field? No: the five field form is the one supported, and a sixth
// field is refused, so a schedule pasted from a system with seconds does
// not silently match a different time.
func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("表达式为空")
	}
	if strings.HasPrefix(expr, "@") {
		macro := strings.ToLower(strings.SplitN(expr, " ", 2)[0])
		switch macro {
		case "@yearly", "@annually":
			expr = "0 0 1 1 *"
		case "@monthly":
			expr = "0 0 1 * *"
		case "@weekly":
			expr = "0 0 * * 0"
		case "@daily", "@midnight":
			expr = "0 0 * * *"
		case "@hourly":
			expr = "0 * * * *"
		default:
			if strings.HasPrefix(macro, "@every") {
				rest := strings.TrimSpace(strings.TrimPrefix(expr, "@every"))
				d, err := time.ParseDuration(rest)
				if err != nil || d < time.Minute {
					return nil, fmt.Errorf("@every 时长无效：%s", rest)
				}
				minutes := int(d / time.Minute)
				if minutes%60 != 0 || minutes < 60 {
					return nil, fmt.Errorf("@every 需为整小时：%s", rest)
				}
				expr = fmt.Sprintf("0 */%d * * *", minutes/60)
			} else {
				return nil, fmt.Errorf("无法识别的表达式：%s", expr)
			}
		}
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("表达式需要 5 个字段（分 时 日 月 周），收到 %d 个", len(fields))
	}
	var err error
	s := &Schedule{}
	if s.minute, err = parseField(fields[0], 0, 59, nil); err != nil {
		return nil, err
	}
	if s.hour, err = parseField(fields[1], 0, 23, nil); err != nil {
		return nil, err
	}
	if s.day, err = parseField(fields[2], 1, 31, nil); err != nil {
		return nil, err
	}
	if s.month, err = parseField(fields[3], 1, 12, monthNames); err != nil {
		return nil, err
	}
	if s.weekday, err = parseField(fields[4], 0, 7, weekdayNames); err != nil {
		return nil, err
	}
	// A 7 written for Sunday is folded into 0, so both spellings match.
	// The fold overwrites instead of adding, so that the unrestricted
	// field still carries exactly the seven values.
	if s.weekday.has(7) {
		delete(s.weekday.values, 7)
		s.weekday.values[0] = true
	}
	return s, nil
}

// Matches reports whether the time given falls into the schedule. The time
// is read in the zone it carries, so the caller hands in a time that was
// already converted to the zone of the program.
//
// When both the day and the weekday are restricted — neither is `*` — the
// fields are OR-ed the way vixie cron does it; when one of them is `*` the
// other one alone decides. A set that carries every value of the field
// counts as unrestricted.
func (s *Schedule) Matches(t time.Time) bool {
	if !s.minute.has(t.Minute()) || !s.hour.has(t.Hour()) || !s.month.has(int(t.Month())) {
		return false
	}
	dayRestricted := s.day.len() != 31
	weekdayRestricted := s.weekday.len() != 7
	if dayRestricted && weekdayRestricted {
		return s.day.has(t.Day()) || s.weekday.has(int(t.Weekday()))
	}
	if dayRestricted {
		return s.day.has(t.Day())
	}
	if weekdayRestricted {
		return s.weekday.has(int(t.Weekday()))
	}
	return true
}

// Next returns the next time after from that matches the schedule, walking
// forward but skipping the spans a field cannot match, so that a yearly
// schedule is found without scanning the year. The zone of from is kept.
func (s *Schedule) Next(from time.Time) time.Time {
	t := from.Truncate(time.Minute).Add(time.Minute)
	limit := from.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !s.month.has(int(t.Month())) {
			// Jump to the first day of the next month.
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
			continue
		}
		if !s.dayMatch(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
			continue
		}
		if !s.hour.has(t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()).Add(time.Hour)
			continue
		}
		if !s.minute.has(t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

// dayMatch applies the day and weekday fields to a time, with the OR rule
// of vixie cron when both are restricted.
func (s *Schedule) dayMatch(t time.Time) bool {
	dayRestricted := s.day.len() != 31
	weekdayRestricted := s.weekday.len() != 7
	if dayRestricted && weekdayRestricted {
		return s.day.has(t.Day()) || s.weekday.has(int(t.Weekday()))
	}
	if dayRestricted {
		return s.day.has(t.Day())
	}
	if weekdayRestricted {
		return s.weekday.has(int(t.Weekday()))
	}
	return true
}

// Describe names the schedule in plain words, for the list of strategies.
func Describe(expr string) string {
	expr = strings.TrimSpace(expr)
	switch strings.ToLower(expr) {
	case "0 0 1 * *", "@monthly":
		return "每月 1 日 00:00"
	case "0 0 * * *", "@daily":
		return "每天 00:00"
	case "0 9 * * 1":
		return "每周一 09:00"
	case "0 9 1 * *":
		return "每月 1 日 09:00"
	}
	s, err := Parse(expr)
	if err != nil {
		return expr
	}
	if s.minute.len() == 1 && s.hour.len() == 1 && s.day.len() == 31 && s.month.len() == 12 && s.weekday.len() == 7 {
		return fmt.Sprintf("每天 %02d:%02d", only(s.hour), only(s.minute))
	}
	if s.minute.len() == 1 && s.hour.len() == 1 && s.month.len() == 12 && s.weekday.len() == 7 {
		return fmt.Sprintf("每月 %d 日 %02d:%02d", only(s.day), only(s.hour), only(s.minute))
	}
	return expr
}

func (f fieldSet) len() int { return len(f.values) }

// only returns the single value of a field with exactly one entry.
func only(f fieldSet) int {
	for v := range f.values {
		return v
	}
	return 0
}

// ParseErrors is a small helper the API uses to validate an expression and
// turn a parse failure into a message in the language of the interface.
func Validate(expr string) error {
	_, err := Parse(expr)
	return err
}
