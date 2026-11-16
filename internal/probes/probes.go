// Пакет probes — конструктор eBPF-пробников (курсовая).
// Из смысловых параметров (что наблюдаем, кого фильтруем, что считаем,
// как часто) строит строку мини-языка и готовый скрипт bpftrace.
// Произвольный код пользователь ввести не может: скрипт собирается
// только из проверенных шаблонов этого файла.
package probes

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/msdremted-a11y/MiSPISIT/internal/storage"
)

// Event — тип наблюдаемого события ядра.
type Event struct {
	Code   string // то, что лежит в БД и в DSL
	Title  string // как показываем в форме
	Tracep string // точка присоединения bpftrace
}

// Agg — способ агрегации событий за интервал.
type Agg struct {
	Code  string
	Title string
	Unit  string // единицы замера для подписей
}

// Events — каталог событий конструктора.
var Events = map[string]Event{
	"syscall_read": {"syscall_read", "Чтение из файлов (системный вызов read)", "syscalls:sys_enter_read"},
	"disk_io":      {"disk_io", "Дисковые операции (запросы к блочному устройству)", "block:block_rq_issue"},
	"file_open":    {"file_open", "Открытие файлов (системный вызов openat)", "syscalls:sys_enter_openat"},
	"net_connect":  {"net_connect", "Исходящие TCP-соединения", "tcp:tcp_connect"},
}

// Aggs — каталог агрегаций.
var Aggs = map[string]Agg{
	"count":          {"count", "Количество событий", "шт/интервал"},
	"bytes":          {"bytes", "Суммарный объём данных", "байт/интервал"},
	"avg_latency_ms": {"avg_latency_ms", "Средняя задержка", "мс"},
}

// allowedAggs — какие агрегации допустимы для какого события.
var allowedAggs = map[string][]string{
	"syscall_read": {"count", "avg_latency_ms"},
	"disk_io":      {"count", "bytes"},
	"file_open":    {"count", "avg_latency_ms"},
	"net_connect":  {"count"},
}

// commRe — допустимое имя процесса (comm в ядре ограничен 15 символами).
var commRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,15}$`)

// AllowedAggs возвращает допустимые агрегации события для формы.
func AllowedAggs(event string) []Agg {
	var out []Agg
	for _, code := range allowedAggs[event] {
		out = append(out, Aggs[code])
	}
	return out
}

// EventCodes возвращает коды событий в стабильном порядке.
func EventCodes() []string {
	codes := make([]string, 0, len(Events))
	for c := range Events {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}

// Build проверяет параметры и собирает пробник: DSL и скрипт bpftrace.
// Возвращает текст ошибки для пользователя, если параметры недопустимы.
func Build(name, event, process, agg string, intervalS int) (storage.Probe, string) {
	name = strings.TrimSpace(name)
	process = strings.TrimSpace(process)

	if name == "" {
		return storage.Probe{}, "укажите имя пробника"
	}
	if !utf8.ValidString(name) || !utf8.ValidString(process) {
		return storage.Probe{}, "имя и процесс должны быть в кодировке UTF-8"
	}
	ev, ok := Events[event]
	if !ok {
		return storage.Probe{}, "неизвестный тип события: " + event
	}
	ag, ok := Aggs[agg]
	if !ok {
		return storage.Probe{}, "неизвестная агрегация: " + agg
	}
	valid := false
	for _, a := range allowedAggs[event] {
		if a == agg {
			valid = true
		}
	}
	if !valid {
		return storage.Probe{}, fmt.Sprintf("агрегация «%s» не подходит для события «%s»", ag.Title, ev.Title)
	}
	if process != "" && !commRe.MatchString(process) {
		return storage.Probe{}, "имя процесса: до 15 символов, только латиница, цифры, точка, дефис и подчёркивание"
	}
	if intervalS < 1 || intervalS > 3600 {
		return storage.Probe{}, "интервал должен быть от 1 до 3600 секунд"
	}

	pr := storage.Probe{
		Name:      name,
		Event:     event,
		Process:   process,
		Agg:       agg,
		IntervalS: intervalS,
		Status:    "active",
	}
	pr.DSL = dsl(pr)
	pr.Bpftrace = bpftrace(pr)
	return pr, ""
}

// dsl собирает строку мини-языка из параметров пробника.
// Грамматика: on <event> [where process = <comm>] every <N>s <agg>.
func dsl(pr storage.Probe) string {
	var b strings.Builder
	b.WriteString("on " + pr.Event)
	if pr.Process != "" {
		b.WriteString(" where process = " + pr.Process)
	}
	fmt.Fprintf(&b, " every %ds %s", pr.IntervalS, pr.Agg)
	return b.String()
}

// filter возвращает условие bpftrace по имени процесса или пустую строку.
func filter(pr storage.Probe) string {
	if pr.Process == "" {
		return ""
	}
	return fmt.Sprintf("/comm == \"%s\"/", pr.Process)
}

// bpftrace собирает скрипт из шаблона. Каждый интервал скрипт печатает
// одну строку "VALUE <число>" — её читает probe-runner и пишет в базу.
func bpftrace(pr storage.Probe) string {
	tp := Events[pr.Event].Tracep
	f := filter(pr)
	iv := fmt.Sprintf("interval:s:%d", pr.IntervalS)

	switch pr.Agg {
	case "count":
		return fmt.Sprintf(
			"tracepoint:%s %s { @c = count(); }\n%s { print(@c); clear(@c); }",
			tp, f, iv)
	case "bytes":
		// сумма байт по дисковым запросам
		return fmt.Sprintf(
			"tracepoint:%s %s { @b = sum(args->bytes); }\n%s { print(@b); clear(@b); }",
			tp, f, iv)
	case "avg_latency_ms":
		// средняя задержка: вход в вызов запоминает время, выход считает дельту;
		// stats хранит count/average/total, runner берёт average (нс) и переводит в мс
		exit := strings.Replace(tp, "sys_enter", "sys_exit", 1)
		pred := "@s[tid]"
		if pr.Process != "" {
			pred = fmt.Sprintf("comm == \"%s\" && @s[tid]", pr.Process)
		}
		return fmt.Sprintf(
			"tracepoint:%s %s { @s[tid] = nsecs; }\n"+
				"tracepoint:%s /%s/ { @lat = stats(nsecs - @s[tid]); delete(@s[tid]); }\n"+
				"%s { print(@lat); clear(@lat); }",
			tp, f, exit, pred, iv)
	}
	return ""
}
