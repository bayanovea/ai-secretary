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
		{name: "часы", in: "1ч", want: 1.0 / HoursPerDay},
		{name: "рабочий день в часах", in: "8ч", want: 1},
		{name: "много часов", in: "36ч", want: 4.5},
		{name: "дни", in: "5д", want: 5},
		{name: "один день", in: "1д", want: 1},
		{name: "недели", in: "2н", want: 14},
		{name: "дробные дни через точку", in: "1.5д", want: 1.5},
		{name: "дробные дни через запятую", in: "1,5д", want: 1.5},
		{name: "дробные часы", in: "0.5ч", want: 0.5 / HoursPerDay},
		{name: "дробные недели", in: "0.5н", want: 3.5},
		{name: "ноль часов", in: "0ч", want: 0},
		{name: "ноль дней", in: "0д", want: 0},
		{name: "пробелы по краям", in: "  3д  ", want: 3},
		{name: "пробел перед единицей", in: "3 д", want: 3},
		{name: "отрицательные часы", in: "-1ч", wantErr: true},
		{name: "отрицательные дни", in: "-2д", wantErr: true},
		{name: "без единицы", in: "5", wantErr: true},
		{name: "чужая единица", in: "5h", wantErr: true},
		{name: "мусор", in: "скоро", wantErr: true},
		{name: "единица без числа", in: "д", wantErr: true},
		{name: "пусто", in: "", wantErr: true},
		{name: "только пробелы", in: "   ", wantErr: true},
		{name: "две единицы", in: "1дд", wantErr: true},
		{name: "бесконечность", in: "Infд", wantErr: true},
		{name: "не число", in: "NaNд", wantErr: true},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseEffort(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ParseEffort(%q) = %v, ожидалась ошибка", c.in, got)
				}
				if !errors.Is(err, ErrInvalidEffort) {
					t.Fatalf("ParseEffort(%q) вернул ошибку %v, ожидалась ErrInvalidEffort", c.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEffort(%q) вернул ошибку: %v", c.in, err)
			}
			if math.Abs(got.Days()-c.want) > epsilon {
				t.Fatalf("ParseEffort(%q) = %v дней, ожидалось %v", c.in, got.Days(), c.want)
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
		{name: "ноль", in: 0, want: "0ч"},
		{name: "час", in: 1.0 / HoursPerDay, want: "1ч"},
		{name: "полдня в часах", in: 4.0 / HoursPerDay, want: "4ч"},
		{name: "день", in: 1, want: "1д"},
		{name: "пять дней", in: 5, want: "5д"},
		{name: "неделя", in: 7, want: "1н"},
		{name: "две недели", in: 14, want: "2н"},
		{name: "девять дней", in: 9, want: "9д"},
		{name: "полтора дня", in: 1.5, want: "1.5д"},
		{name: "четыре с половиной дня", in: 4.5, want: "4.5д"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.String(); got != c.want {
				t.Fatalf("Effort(%v).String() = %q, ожидалось %q", float64(c.in), got, c.want)
			}
		})
	}
}

// TestEffortRoundTrip is the guarantee the renderer depends on: whatever the
// artifact says, writing it back out and reading it again yields the same
// amount of work, even when the unit is normalised along the way.
func TestEffortRoundTrip(t *testing.T) {
	inputs := []string{"1ч", "4ч", "8ч", "36ч", "0.5ч", "1д", "5д", "9д", "1.5д", "1,5д", "1н", "2н", "0.5н", "0ч"}

	for _, in := range inputs {
		in := in
		t.Run(in, func(t *testing.T) {
			first, err := ParseEffort(in)
			if err != nil {
				t.Fatalf("ParseEffort(%q) вернул ошибку: %v", in, err)
			}
			rendered := first.String()
			second, err := ParseEffort(rendered)
			if err != nil {
				t.Fatalf("ParseEffort(%q) после рендера %q вернул ошибку: %v", rendered, in, err)
			}
			if math.Abs(first.Days()-second.Days()) > epsilon {
				t.Fatalf("round-trip %q → %q: %v дней стало %v", in, rendered, first.Days(), second.Days())
			}
			if again := second.String(); again != rendered {
				t.Fatalf("round-trip %q нестабилен: %q затем %q", in, rendered, again)
			}
		})
	}
}

func TestEffortIsZero(t *testing.T) {
	if !Effort(0).IsZero() {
		t.Fatal("Effort(0).IsZero() = false")
	}
	if Effort(1.0 / HoursPerDay).IsZero() {
		t.Fatal("час работы посчитан нулевым объёмом")
	}
}
