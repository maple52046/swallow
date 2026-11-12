package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a parsed five-field cron expression (minute, hour, day of month, month, day of week),
// read in one time zone. Each field accepts `*`, a value, a range `a-b`, a list `a,b`, and a step
// `*/n` or `a-b/n`; day of week is 0–7 with both 0 and 7 Sunday. As in cron, when both day fields
// are restricted a day matches either of them.
type Cron struct {
	minutes, hours, days, months, weekdays [64]bool
	daysRestricted, weekdaysRestricted     bool
	location                               *time.Location
}

// cronSearchLimit bounds how far Next looks ahead; an expression that matches no time within it
// (for example 30 February) has no next time.
const cronSearchLimit = 5 * 366 * 24 * time.Hour

// ParseCron parses expression in the IANA time zone timezone ("" is UTC). A malformed expression or
// an unknown zone is ErrInvalidRequest.
func ParseCron(expression, timezone string) (*Cron, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return nil, fmt.Errorf("%w: cron %q must have five fields (minute hour day-of-month month day-of-week)", ErrInvalidRequest, expression)
	}
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: time zone %q is not an IANA time zone", ErrInvalidRequest, timezone)
	}
	cron := &Cron{location: location}
	specs := []struct {
		field    string
		min, max int
		set      *[64]bool
	}{
		{fields[0], 0, 59, &cron.minutes}, {fields[1], 0, 23, &cron.hours}, {fields[2], 1, 31, &cron.days},
		{fields[3], 1, 12, &cron.months}, {fields[4], 0, 7, &cron.weekdays},
	}
	for _, spec := range specs {
		if err := parseCronField(spec.field, spec.min, spec.max, spec.set); err != nil {
			return nil, fmt.Errorf("%w: cron %q: %v", ErrInvalidRequest, expression, err)
		}
	}
	if cron.weekdays[7] {
		cron.weekdays[0] = true
	}
	cron.daysRestricted, cron.weekdaysRestricted = fields[2] != "*", fields[4] != "*"
	return cron, nil
}

func parseCronField(field string, min, max int, set *[64]bool) error {
	for _, part := range strings.Split(field, ",") {
		step := 1
		if base, stepText, ok := strings.Cut(part, "/"); ok {
			value, err := strconv.Atoi(stepText)
			if err != nil || value < 1 {
				return fmt.Errorf("step %q is not a positive number", stepText)
			}
			part, step = base, value
		}
		low, high := min, max
		if part != "*" {
			lowText, highText, isRange := strings.Cut(part, "-")
			var err error
			if low, err = strconv.Atoi(lowText); err != nil {
				return fmt.Errorf("%q is not a number", lowText)
			}
			high = low
			if isRange {
				if high, err = strconv.Atoi(highText); err != nil {
					return fmt.Errorf("%q is not a number", highText)
				}
			} else if step > 1 {
				high = max
			}
		}
		if low < min || high > max || low > high {
			return fmt.Errorf("%q is outside %d-%d", part, min, max)
		}
		for value := low; value <= high; value += step {
			set[value] = true
		}
	}
	return nil
}

// Next returns the first time strictly after after that the expression matches, in the
// expression's time zone (returned in UTC), or false when none comes within five years.
func (c *Cron) Next(after time.Time) (time.Time, bool) {
	t := after.In(c.location).Truncate(time.Minute).Add(time.Minute)
	limit := after.Add(cronSearchLimit)
	for t.Before(limit) {
		if !c.months[int(t.Month())] {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, c.location)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, c.location)
			continue
		}
		if !c.hours[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, c.location)
			continue
		}
		if !c.minutes[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t.UTC(), true
	}
	return time.Time{}, false
}

func (c *Cron) dayMatches(t time.Time) bool {
	day, weekday := c.days[t.Day()], c.weekdays[int(t.Weekday())]
	switch {
	case c.daysRestricted && c.weekdaysRestricted:
		return day || weekday
	case c.daysRestricted:
		return day
	case c.weekdaysRestricted:
		return weekday
	}
	return true
}
