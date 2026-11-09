package model

import (
	"strconv"
	"strings"
)

// DateForm says how a date reads.
type DateForm string

// The forms of a date.
const (
	DateExact  DateForm = "exact"
	DateBefore DateForm = "before"
	DateAfter  DateForm = "after"
	DateAbout  DateForm = "about"
	DateRange  DateForm = "range"
	DateSpan   DateForm = "span"
	DateText   DateForm = "text"
)

// DatePoint is a calendar date. A zero month or day means "not known".
type DatePoint struct {
	Year  int `json:"y"`
	Month int `json:"m,omitempty"`
	Day   int `json:"d,omitempty"`
}

// DateView is the form of a date that the API sends to a client.
type DateView struct {
	Form     DateForm   `json:"form"`
	Start    *DatePoint `json:"start,omitempty"`
	Stop     *DatePoint `json:"stop,omitempty"`
	Text     string     `json:"text,omitempty"`
	Calendar string     `json:"calendar,omitempty"`
	Quality  string     `json:"quality,omitempty"`
}

// parsePoint reads YYYY, YYYY-MM or YYYY-MM-DD. A year may be negative or
// unknown ("????"). Unknown parts become zero.
func parsePoint(s string) *DatePoint {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	parts := strings.Split(s, "-")
	if len(parts) > 3 {
		return nil
	}
	values := [3]int{}
	for i, part := range parts {
		if strings.Trim(part, "?") == "" {
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 {
			return nil
		}
		values[i] = v
	}
	if values[1] > 13 || values[2] > 31 {
		return nil
	}
	year := values[0]
	if negative {
		year = -year
	}
	return &DatePoint{Year: year, Month: values[1], Day: values[2]}
}

// View returns the date in the form that a client can show.
// It returns nil if the object has no date.
func (d Dated) View() *DateView {
	switch {
	case d.DateVal != nil:
		v := d.DateVal
		form := DateExact
		switch v.Type {
		case "before":
			form = DateBefore
		case "after":
			form = DateAfter
		case "about":
			form = DateAbout
		}
		point := parsePoint(v.Val)
		if point == nil {
			return &DateView{Form: DateText, Text: v.Val}
		}
		return &DateView{Form: form, Start: point, Calendar: v.CFormat, Quality: v.Quality}
	case d.DateRange != nil:
		return pairView(DateRange, d.DateRange)
	case d.DateSpan != nil:
		return pairView(DateSpan, d.DateSpan)
	case d.DateStr != nil:
		return &DateView{Form: DateText, Text: d.DateStr.Val}
	}
	return nil
}

func pairView(form DateForm, p *DatePair) *DateView {
	start, stop := parsePoint(p.Start), parsePoint(p.Stop)
	if start == nil && stop == nil {
		return &DateView{Form: DateText, Text: strings.TrimSpace(p.Start + " " + p.Stop)}
	}
	return &DateView{Form: form, Start: start, Stop: stop, Calendar: p.CFormat, Quality: p.Quality}
}

// gregorianYear converts a year from another calendar to an approximate
// Gregorian year. Heirfold uses it to sort and to estimate ages.
func gregorianYear(year int, calendar string) int {
	if year == 0 {
		return 0
	}
	switch strings.ToLower(calendar) {
	case "hebrew":
		return year - 3760
	case "islamic":
		return 622 + year*97/100
	case "persian":
		return year + 621
	case "french republican":
		return year + 1791
	}
	return year
}

// uncertainty is the margin for a date that its source marks as not exact.
const uncertainty = 10

// LatestYear returns the latest Gregorian year that the date can mean. It
// returns false if the date has no upper bound: a date "after" a year, a range
// with no end, or text.
func (v *DateView) LatestYear() (int, bool) {
	if v == nil || v.Form == DateAfter || v.Form == DateText {
		return 0, false
	}
	point := v.Start
	if v.Form == DateRange || v.Form == DateSpan {
		point = v.Stop
	}
	if point == nil || point.Year == 0 {
		return 0, false
	}
	year := gregorianYear(point.Year, v.Calendar)
	if v.Form == DateAbout || v.Quality != "" {
		year += uncertainty
	}
	return year, true
}

// SortKey returns a number that orders dates in time. Zero means no date.
func (v *DateView) SortKey() int64 {
	if v == nil {
		return 0
	}
	point := v.Start
	if point == nil || point.Year == 0 {
		point = v.Stop
	}
	if point == nil || point.Year == 0 {
		return 0
	}
	year := gregorianYear(point.Year, v.Calendar)
	key := int64(year)*10000 + int64(point.Month)*100 + int64(point.Day)
	if key == 0 {
		return 1
	}
	return key
}
