package task

import (
	"errors"
	"math"
	"testing"
)

const epsilon = 1e-9

func TestParseEffort(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    float64
		wantErr bool
	}{
		{name: "hours", in: "1h", want: 1.0 / HoursPerDay},
		{name: "working day in hours", in: "8h", want: 1},
		{name: "many hours", in: "36h", want: 4.5},
		{name: "days", in: "5d", want: 5},
		{name: "one day", in: "1d", want: 1},
		{name: "weeks", in: "2w", want: 14},
		{name: "fractional days with a dot", in: "1.5d", want: 1.5},
		{name: "fractional days with a comma", in: "1,5d", want: 1.5},
		{name: "fractional hours", in: "0.5h", want: 0.5 / HoursPerDay},
		{name: "fractional weeks", in: "0.5w", want: 3.5},
		{name: "zero hours", in: "0h", want: 0},
		{name: "zero days", in: "0d", want: 0},
		{name: "surrounding whitespace", in: "  3d  ", want: 3},
		{name: "space before the unit", in: "3 d", want: 3},
		{name: "negative hours", in: "-1h", wantErr: true},
		{name: "negative days", in: "-2d", wantErr: true},
		{name: "no unit", in: "5", wantErr: true},
		{name: "foreign unit", in: "5x", wantErr: true},
		{name: "garbage", in: "soon", wantErr: true},
		{name: "unit without a number", in: "d", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "two units", in: "1dd", wantErr: true},
		{name: "infinity", in: "Infd", wantErr: true},
		{name: "not a number", in: "NaNd", wantErr: true},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseEffort(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseEffort(%q) = %v, want an error", c.in, got)
				}
				if !errors.Is(err, ErrInvalidEffort) {
					t.Fatalf("ParseEffort(%q) returned an error %v, want ErrInvalidEffort", c.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEffort(%q) returned an error: %v", c.in, err)
			}
			if math.Abs(got.Days()-c.want) > epsilon {
				t.Fatalf("ParseEffort(%q) = %v days, want %v", c.in, got.Days(), c.want)
			}
		})
	}
}

func TestEffortString(t *testing.T) {
	cases := []struct {
		name string
		in   Effort
		want string
	}{
		{name: "zero", in: 0, want: "0h"},
		{name: "hour", in: 1.0 / HoursPerDay, want: "1h"},
		{name: "half a day in hours", in: 4.0 / HoursPerDay, want: "4h"},
		{name: "day", in: 1, want: "1d"},
		{name: "five days", in: 5, want: "5d"},
		{name: "week", in: 7, want: "1w"},
		{name: "two weeks", in: 14, want: "2w"},
		{name: "nine days", in: 9, want: "9d"},
		{name: "a day and a half", in: 1.5, want: "1.5d"},
		{name: "four and a half days", in: 4.5, want: "4.5d"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.String(); got != c.want {
				t.Fatalf("Effort(%v).String() = %q, want %q", float64(c.in), got, c.want)
			}
		})
	}
}

// TestEffortRoundTrip is the guarantee the renderer depends on: whatever the
// artifact says, writing it back out and reading it again yields the same
// amount of work, even when the unit is normalised along the way.
func TestEffortRoundTrip(t *testing.T) {
	inputs := []string{"1h", "4h", "8h", "36h", "0.5h", "1d", "5d", "9d", "1.5d", "1,5d", "1w", "2w", "0.5w", "0h"}

	for _, in := range inputs {
		in := in
		t.Run(in, func(t *testing.T) {
			first, err := ParseEffort(in)
			if err != nil {
				t.Fatalf("ParseEffort(%q) returned an error: %v", in, err)
			}
			rendered := first.String()
			second, err := ParseEffort(rendered)
			if err != nil {
				t.Fatalf("ParseEffort(%q) after rendering %q returned an error: %v", rendered, in, err)
			}
			if math.Abs(first.Days()-second.Days()) > epsilon {
				t.Fatalf("round-trip %q → %q: %v days became %v", in, rendered, first.Days(), second.Days())
			}
			if again := second.String(); again != rendered {
				t.Fatalf("round-trip %q unstable: %q then %q", in, rendered, again)
			}
		})
	}
}

func TestEffortIsZero(t *testing.T) {
	if !Effort(0).IsZero() {
		t.Fatal("Effort(0).IsZero() = false")
	}
	if Effort(1.0 / HoursPerDay).IsZero() {
		t.Fatal("an hour of work came out as zero effort")
	}
}
