package task

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// HoursPerDay is the length of a working day. Effort written as `Nч` is a
// fraction of it: `4ч` is half a day of work, not four twenty-fourths of one.
const HoursPerDay = 8

// Unit suffixes accepted by ParseEffort, and emitted by Effort.String.
const (
	unitHours = "ч"
	unitDays  = "д"
	unitWeeks = "н"
)

// ErrInvalidEffort is returned by ParseEffort for anything it cannot read as a
// non-negative amount of work.
var ErrInvalidEffort = errors.New("некорректный объём")

// Effort is an amount of work measured in days. Fractions are meaningful: an
// hour of work is 1/HoursPerDay.
//
// It is stored as a single number rather than as (value, unit) so that the
// schedule package can do arithmetic on it directly; the original unit is
// recovered by String, which picks the largest unit that divides evenly.
type Effort float64

// ParseEffort reads `Nч` (hours), `Nд` (days) or `Nн` (weeks). The number may be
// fractional, with either a dot or a Russian comma as the separator. Zero is
// allowed; negative amounts, missing units and garbage are not.
func ParseEffort(s string) (Effort, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("%w: пусто", ErrInvalidEffort)
	}

	var unit string
	var perUnit float64
	switch {
	case strings.HasSuffix(trimmed, unitHours):
		unit, perUnit = unitHours, 1.0/HoursPerDay
	case strings.HasSuffix(trimmed, unitDays):
		unit, perUnit = unitDays, 1
	case strings.HasSuffix(trimmed, unitWeeks):
		unit, perUnit = unitWeeks, 7
	default:
		return 0, fmt.Errorf("%w %q: ожидается суффикс ч, д или н", ErrInvalidEffort, s)
	}

	number := strings.TrimSpace(strings.TrimSuffix(trimmed, unit))
	number = strings.Replace(number, ",", ".", 1)
	value, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, fmt.Errorf("%w %q: не число", ErrInvalidEffort, s)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%w %q: не конечное число", ErrInvalidEffort, s)
	}
	if value < 0 {
		return 0, fmt.Errorf("%w %q: отрицательный", ErrInvalidEffort, s)
	}

	return Effort(value * perUnit), nil
}

// IsZero reports whether no effort was recorded. Callers treat that the same as
// an absent объём: the latest start date cannot be computed without it.
func (e Effort) IsZero() bool { return e == 0 }

// Days returns the effort as a (possibly fractional) number of days.
func (e Effort) Days() float64 { return float64(e) }

// String renders the effort back into artifact form: whole weeks where they are
// exact, then days, and hours only for less than a day of work. It round-trips
// through ParseEffort — the value survives, though the unit may be normalised
// (`36ч` comes back as `4.5д`).
func (e Effort) String() string {
	days := float64(e)
	switch {
	case days == 0:
		return "0" + unitHours
	case days >= 7 && isWhole(days/7):
		return formatNumber(days/7) + unitWeeks
	case days >= 1:
		return formatNumber(days) + unitDays
	case isWhole(days * HoursPerDay):
		return formatNumber(days*HoursPerDay) + unitHours
	default:
		return formatNumber(days) + unitDays
	}
}

// isWhole tolerates the rounding error of the hours↔days conversion: 3ч is
// 0.375 days exactly, but not every fraction is so kind.
func isWhole(v float64) bool {
	return math.Abs(v-math.Round(v)) < 1e-9
}

// formatNumber prints a float without a trailing `.0`, so 5 days is `5д` and
// not `5.0д`.
func formatNumber(v float64) string {
	if isWhole(v) {
		return strconv.FormatFloat(math.Round(v), 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
