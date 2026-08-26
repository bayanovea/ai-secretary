package task

import (
	"errors"
	"testing"
	"time"
)

func TestStatusString(t *testing.T) {
	cases := []struct {
		in   Status
		want string
	}{
		{in: StatusNoDue, want: "без срока"},
		{in: StatusCalm, want: "спокойно"},
		{in: StatusSoon, want: "скоро"},
		{in: StatusStartNow, want: "пора начинать"},
		{in: StatusOverdue, want: "просрочено"},
		{in: Status(42), want: "неизвестно"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.want, func(t *testing.T) {
			if got := c.in.String(); got != c.want {
				t.Fatalf("Status(%d).String() = %q, ожидалось %q", int(c.in), got, c.want)
			}
		})
	}
}

// TestStatusSeverityOrder pins the constant order: the sort in the store treats
// a larger Status as more urgent, and the zero value must be the calm one.
func TestStatusSeverityOrder(t *testing.T) {
	if StatusNoDue != 0 {
		t.Fatalf("StatusNoDue = %d, ожидалось нулевое значение", int(StatusNoDue))
	}
	if !(StatusCalm < StatusSoon && StatusSoon < StatusStartNow && StatusStartNow < StatusOverdue) {
		t.Fatal("статусы объявлены не по возрастанию срочности")
	}
}

func validTask() Task {
	due := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	return Task{
		ID:      "T-001",
		Title:   "Подготовить отчёт за Q3",
		Section: "Работа",
		Tier:    1,
		From:    "Анна Петрова",
		Due:     &due,
		Effort:  5,
		Context: "@компьютер",
		Labels:  []string{"отчётность", "финансы"},
		Created: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Task)
		wantErr error
	}{
		{name: "корректная задача", mutate: func(*Task) {}},
		{name: "минимальная задача", mutate: func(tk *Task) {
			tk.Due = nil
			tk.Effort = 0
			tk.Context = ""
			tk.Labels = nil
		}},
		{name: "тир 3", mutate: func(tk *Task) { tk.Tier = 3 }},
		{name: "от себя", mutate: func(tk *Task) { tk.From = SelfAssigned }},
		{name: "длинный идентификатор", mutate: func(tk *Task) { tk.ID = "T-1234" }},

		{name: "пустой идентификатор", mutate: func(tk *Task) { tk.ID = "" }, wantErr: ErrInvalidID},
		{name: "идентификатор без префикса", mutate: func(tk *Task) { tk.ID = "001" }, wantErr: ErrInvalidID},
		{name: "идентификатор с коротким номером", mutate: func(tk *Task) { tk.ID = "T-1" }, wantErr: ErrInvalidID},
		{name: "идентификатор не в верхнем регистре", mutate: func(tk *Task) { tk.ID = "t-001" }, wantErr: ErrInvalidID},
		{name: "идентификатор с хвостом", mutate: func(tk *Task) { tk.ID = "T-001x" }, wantErr: ErrInvalidID},

		{name: "тир ноль", mutate: func(tk *Task) { tk.Tier = 0 }, wantErr: ErrInvalidTier},
		{name: "тир слишком большой", mutate: func(tk *Task) { tk.Tier = 4 }, wantErr: ErrInvalidTier},
		{name: "тир отрицательный", mutate: func(tk *Task) { tk.Tier = -1 }, wantErr: ErrInvalidTier},

		{name: "пустое название", mutate: func(tk *Task) { tk.Title = "" }, wantErr: ErrEmptyTitle},
		{name: "название из пробелов", mutate: func(tk *Task) { tk.Title = "   " }, wantErr: ErrEmptyTitle},

		{name: "пустое от", mutate: func(tk *Task) { tk.From = "" }, wantErr: ErrEmptyFrom},
		{name: "от из пробелов", mutate: func(tk *Task) { tk.From = "  " }, wantErr: ErrEmptyFrom},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			tk := validTask()
			c.mutate(&tk)

			err := tk.Validate()
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() вернул ошибку: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, ожидалась %v", c.wantErr)
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, ожидалась %v", err, c.wantErr)
			}
		})
	}
}

func TestHasDue(t *testing.T) {
	tk := validTask()
	if !tk.HasDue() {
		t.Fatal("HasDue() = false для задачи со сроком")
	}
	tk.Due = nil
	if tk.HasDue() {
		t.Fatal("HasDue() = true для задачи без срока")
	}
}
