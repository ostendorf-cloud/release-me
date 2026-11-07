// Copyright 2026 Simon Ostendorf
// SPDX-License-Identifier: Apache-2.0

package changelog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultDateFormat is the date format a version heading carries unless
// "changelog-date-format" names another one: "2026-11-04".
const DefaultDateFormat = "%Y-%m-%d"

// The strftime directives FormatDate and ParseDate understand, release-
// please's date-format syntax: %Y four digit year, %y two digit year, %m
// two digit month, %d two digit day, %e day without its leading zero, %B
// and %b full and abbreviated English month name, %A and %a full and
// abbreviated English weekday name, %j three digit day of the year, %%
// a literal "%".
const dateDirectives = "YymdeBbAaj%"

// dateCheck is the dates ValidateDateFormat renders and reads back: days
// and months with one and two digits, a leap day and the year's last day.
var dateCheck = []time.Time{
	time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC),
	time.Date(2026, time.November, 14, 0, 0, 0, 0, time.UTC),
	time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC),
	time.Date(2029, time.December, 31, 0, 0, 0, 0, time.UTC),
}

// ValidateDateFormat rejects a date format a version heading cannot carry:
// one that is empty, uses a directive outside dateDirectives, holds a
// parenthesis or a line break, which end the heading's "(<date>)" early,
// or renders a date ParseDate cannot read back to the same text, such as
// "%e%m", where "1" and "11" run together.
func ValidateDateFormat(format string) error {
	switch {
	case format == "":
		return errors.New("is empty")
	case strings.ContainsAny(format, "()\r\n"):
		return errors.New("holds a parenthesis or a line break, which end the heading's date early")
	}
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		if i+1 == len(format) || !strings.ContainsRune(dateDirectives, rune(format[i+1])) {
			return fmt.Errorf("uses an unknown directive at %q, only %%%s are known",
				format[i:min(i+2, len(format))], strings.Join(strings.Split(dateDirectives, ""), " %"))
		}
		i++
	}
	for _, d := range dateCheck {
		text := FormatDate(d, format)
		back, err := ParseDate(text, format)
		if err != nil || FormatDate(back, format) != text {
			return fmt.Errorf("renders %q, which does not read back to the same date", text)
		}
	}
	return nil
}

// FormatDate renders date in format, a strftime expression of the
// directives dateDirectives lists. Text outside a directive renders as it
// is. "" means DefaultDateFormat.
func FormatDate(date time.Time, format string) string {
	if format == "" {
		format = DefaultDateFormat
	}
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 == len(format) {
			b.WriteByte(format[i])
			continue
		}
		i++
		switch format[i] {
		case 'Y':
			fmt.Fprintf(&b, "%04d", date.Year())
		case 'y':
			fmt.Fprintf(&b, "%02d", date.Year()%100)
		case 'm':
			fmt.Fprintf(&b, "%02d", int(date.Month()))
		case 'd':
			fmt.Fprintf(&b, "%02d", date.Day())
		case 'e':
			b.WriteString(strconv.Itoa(date.Day()))
		case 'B':
			b.WriteString(date.Month().String())
		case 'b':
			b.WriteString(date.Month().String()[:3])
		case 'A':
			b.WriteString(date.Weekday().String())
		case 'a':
			b.WriteString(date.Weekday().String()[:3])
		case 'j':
			fmt.Fprintf(&b, "%03d", date.YearDay())
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}
	return b.String()
}

// ParseDate reads text rendered by FormatDate in format back into a date,
// UTC midnight. A part format leaves out falls back to January, the first
// day, or the year 2000 for a two digit year's century. A weekday is
// matched but never checked against the date. "" means DefaultDateFormat.
func ParseDate(text, format string) (time.Time, error) {
	if format == "" {
		format = DefaultDateFormat
	}
	year, month, day, yday := 0, 1, 1, 0
	rest := text
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 == len(format) {
			if !strings.HasPrefix(rest, format[i:i+1]) {
				return time.Time{}, fmt.Errorf("parse date %q: want %q at %q", text, format[i:i+1], rest)
			}
			rest = rest[1:]
			continue
		}
		i++
		var err error
		switch format[i] {
		case 'Y':
			year, rest, err = digits(rest, 4, 4)
		case 'y':
			year, rest, err = digits(rest, 2, 2)
			year += 2000
		case 'm':
			month, rest, err = digits(rest, 2, 2)
		case 'd':
			day, rest, err = digits(rest, 2, 2)
		case 'e':
			day, rest, err = digits(rest, 1, 2)
		case 'j':
			yday, rest, err = digits(rest, 3, 3)
		case 'B', 'b':
			month, rest, err = name(rest, format[i] == 'b', func(n int) string { return time.Month(n).String() }, 1, 12)
		case 'A', 'a':
			_, rest, err = name(rest, format[i] == 'a', func(n int) string { return time.Weekday(n).String() }, 0, 6)
		case '%':
			if !strings.HasPrefix(rest, "%") {
				err = errors.New(`want "%"`)
			}
			rest = strings.TrimPrefix(rest, "%")
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("parse date %q: %w", text, err)
		}
	}
	if rest != "" {
		return time.Time{}, fmt.Errorf("parse date %q: %q left over", text, rest)
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("parse date %q: no such month or day", text)
	}
	if yday > 0 {
		return time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, yday-1), nil
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
}

// digits reads between minLen and maxLen leading ASCII digits off s.
func digits(s string, minLen, maxLen int) (int, string, error) {
	n := 0
	for n < maxLen && n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n < minLen {
		return 0, s, fmt.Errorf("want %d digits at %q", minLen, s)
	}
	v, _ := strconv.Atoi(s[:n])
	return v, s[n:], nil
}

// name reads the English name nameOf gives one of from to to off s, its
// first three letters only when short, and returns its number.
func name(s string, short bool, nameOf func(int) string, from, to int) (int, string, error) {
	for n := from; n <= to; n++ {
		want := nameOf(n)
		if short {
			want = want[:3]
		}
		if strings.HasPrefix(s, want) {
			return n, s[len(want):], nil
		}
	}
	return 0, s, fmt.Errorf("want a name at %q", s)
}
