package voice

import (
	"fmt"
	"strconv"
	"time"

	"github.com/bayanovea/secretary/internal/grouping"
	"github.com/bayanovea/secretary/internal/schedule"
	"github.com/bayanovea/secretary/internal/task"
)

// Alfred is Альфред Пенниуорт: the butler who has known you since you were
// small. Formal, dry about trivia, and the only one willing to say out loud
// that you have taken on more than a day holds.
//
// He has no state — the wording depends only on the facts he is handed — so the
// zero value is usable and safe to share.
type Alfred struct{}

// Greeting opens the briefing. The wording turns over daily, so two runs on the
// same day read identically and Monday does not sound like Sunday.
func (a Alfred) Greeting(now time.Time) string {
	variants := []string{
		"Доброе утро, сэр. Сегодня %s; позвольте изложить положение дел.",
		"Сэр. Сводка на %s, если вы уже в состоянии её выслушать.",
		"%s, сэр. Кофе подан, календарь открыт.",
		"Сэр, положение дел на %s. Извольте.",
	}
	return fmt.Sprintf(pick(variants, "greeting", dayKey(now)), russianDate(now))
}

// Overdue reports a duty whose deadline passed days ago. The phrasing follows
// the count, so the same duty sounds worse the longer it is left.
func (a Alfred) Overdue(t task.Task, days int) string {
	variants := []string{
		"«%s» просрочено на %s, сэр. Я бы не напоминал, если бы это можно было замять.",
		"Срок «%s» истёк %s назад, сэр. От: %s.",
		"«%s»: %s сверх срока, сэр. Дальше будет только неудобнее.",
		"Сэр, «%s» ждёт уже %s после срока. Полагаю, пора признать это делом, а не намерением.",
	}
	phrase := pick(variants, "overdue", t.ID, strconv.Itoa(days))
	if countPlaceholders(phrase) == 3 {
		return fmt.Sprintf(phrase, t.Title, plural(days, "день", "дня", "дней"), t.From)
	}
	return fmt.Sprintf(phrase, t.Title, plural(days, "день", "дня", "дней"))
}

// StartNow reports a duty that has to be started today to be finished on time.
func (a Alfred) StartNow(t task.Task) string {
	variants := []string{
		"«%s» — начинать сегодня, сэр. Позже уже не успеть.",
		"Сэр, «%s» пора начинать. Именно сегодня, а не «на неделе».",
		"«%s»: последний день, когда начать ещё значит закончить вовремя, сэр.",
		"Если «%s» не начать сегодня, сэр, то к сроку мы придём с извинениями вместо результата.",
	}
	return fmt.Sprintf(pick(variants, "startnow", t.ID, dueKey(t)), t.Title)
}

// Soon reports a duty that has to be started in days days.
func (a Alfred) Soon(t task.Task, days int) string {
	variants := []string{
		"«%s» — начинать через %s, сэр.",
		"Сэр, до начала «%s» остаётся %s.",
		"«%s» подойдёт через %s, сэр. Не срочно, но и не забудьте.",
	}
	return fmt.Sprintf(pick(variants, "soon", t.ID, strconv.Itoa(days)), t.Title, plural(days, "день", "дня", "дней"))
}

// GroupIntro introduces a batch of duties worth doing in one go. The signal
// decides the wording: a shared place is a trip, a shared person a conversation,
// a shared label merely a subject.
func (a Alfred) GroupIntro(g grouping.Group) string {
	var variants []string
	switch g.Kind {
	case grouping.KindContext:
		variants = []string{
			"Раз уж вы окажетесь %s, сэр, — заодно:",
			"Одна поездка, %s, и сразу %s за раз, сэр:",
			"%s, сэр: разумно закрыть всё за один выход:",
		}
	case grouping.KindPerson:
		variants = []string{
			"Раз уж вы говорите с %s, сэр, — заодно:",
			"Один разговор с %s закрывает %s, сэр:",
			"К %s, сэр, у вас накопилось:",
		}
	default:
		variants = []string{
			"Одна тема — %s, сэр. Их удобно закрыть разом:",
			"По теме «%s» скопилось %s, сэр:",
			"%s, сэр: раз уж вы всё равно в это погрузитесь:",
		}
	}
	phrase := pick(variants, "group", g.Kind.String(), g.Key, strconv.Itoa(g.Size()))
	if countPlaceholders(phrase) == 2 {
		return fmt.Sprintf(phrase, g.Key, plural(g.Size(), "дело", "дела", "дел"))
	}
	return fmt.Sprintf(phrase, g.Key)
}

// Complaint voices a flag raised about a duty: the remarks that are not about
// the deadline, and the ones Alfred is least willing to leave unsaid.
func (a Alfred) Complaint(f schedule.Flag, t task.Task) string {
	switch f {
	case schedule.FlagIncomplete:
		variants := []string{
			"«%s» помечено как критичное, сэр, но ни срока, ни объёма. Планировать по вдохновению я не обучен.",
			"Сэр, «%s» — первый тир без срока или без объёма. Скажите мне их, и я скажу, когда начинать.",
			"О «%s» известно только, что это важно, сэр. Этого мало для расписания.",
		}
		return fmt.Sprintf(pick(variants, "incomplete", t.ID), t.Title)
	case schedule.FlagPostponed:
		variants := []string{
			"«%s» перенесено уже %s, сэр. В какой-то момент это перестаёт быть расписанием и становится решением.",
			"Сэр, срок «%s» сдвигался %s. Возможно, дело стоит не отложить, а вычеркнуть.",
			"«%s» откладывалось %s, сэр. Я обязан это озвучить.",
		}
		return fmt.Sprintf(pick(variants, "postponed", t.ID, strconv.Itoa(t.Deferrals)),
			t.Title, plural(t.Deferrals, "раз", "раза", "раз"))
	case schedule.FlagForgotten:
		variants := []string{
			"«%s» лежит без срока с %s, сэр. Полагаю, мы оба делаем вид, что помним о ней.",
			"Сэр, «%s» записано %s и с тех пор не сдвинулось ни на день.",
			"«%s» ждёт с %s, сэр. Либо срок, либо честное «нет».",
		}
		return fmt.Sprintf(pick(variants, "forgotten", t.ID, dayKey(t.Created)), t.Title, russianDate(t.Created))
	default:
		variants := []string{
			"С «%s» что-то не так, сэр, но я затрудняюсь сформулировать что именно.",
			"«%s» требует вашего внимания, сэр, по причине, которую я не берусь назвать.",
		}
		return fmt.Sprintf(pick(variants, "flag", strconv.Itoa(int(f)), t.ID), t.Title)
	}
}

// AllClear is the report for a day with nothing overdue, nothing to start and
// nothing forgotten.
//
// The interface hands this method nothing to vary on, and that is deliberate:
// the one line the butler repeats verbatim is the one that says everything is
// in order.
func (a Alfred) AllClear() string {
	return "Ничего не горит и ничего не забыто, сэр. Редкий день — распорядитесь им по своему усмотрению."
}

// Signoff closes the briefing. Like AllClear it is fixed: a ritual phrase is
// more reassuring than a varied one.
func (a Alfred) Signoff() string {
	return "К вашим услугам, сэр."
}

// dueKey is the seed part standing for a duty's deadline. A duty without one
// still needs a stable seed, hence the explicit word.
func dueKey(t task.Task) string {
	if t.Due == nil {
		return "без-срока"
	}
	return dayKey(*t.Due)
}

// plural renders a count with the Russian form its last digits require:
// 1 день, 2 дня, 5 дней — and 11 дней, which is why the teens are special-cased.
func plural(n int, one, few, many string) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	form := many
	switch {
	case abs%100 >= 11 && abs%100 <= 14:
		form = many
	case abs%10 == 1:
		form = one
	case abs%10 >= 2 && abs%10 <= 4:
		form = few
	}
	return strconv.Itoa(n) + " " + form
}

// russianMonths are the genitive forms used in «27 августа».
var russianMonths = [...]string{
	"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

// russianDate renders a date the way Alfred would say it aloud.
func russianDate(t time.Time) string {
	if t.IsZero() {
		return "неизвестного числа"
	}
	return fmt.Sprintf("%d %s", t.Day(), russianMonths[int(t.Month())-1])
}

// countPlaceholders counts the `%s` verbs in a template, so a variant may take
// either the bare title or the title plus a detail without the caller having to
// keep two parallel lists in step.
func countPlaceholders(s string) int {
	n := 0
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '%' && s[i+1] == 's' {
			n++
		}
	}
	return n
}
