// Пакет alerts — оценка правил алертинга (курсовая).
// Для каждого активного правила проверяются последние N замеров
// (метрика снимка или замер пробника): если все они за порогом,
// открывается инцидент и создаётся уведомление; когда условие
// перестаёт выполняться, инцидент закрывается.
package alerts

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/msdremted-a11y/MiSPISIT/internal/storage"
)

// Store — то, что оценке нужно от хранилища.
type Store interface {
	ListRules() ([]storage.Rule, map[int]string, error)
	ListSnapshots() ([]storage.Snapshot, error)
	ListProbeSamples(probeID, limit int) ([]storage.ProbeSample, error)
	OpenIncidentByRule(ruleID int) (storage.Incident, error)
	OpenIncident(ruleID int, at time.Time, value float64) (int, error)
	TouchIncident(id int, value float64) error
	CloseIncident(id int, at time.Time) error
	AddNotification(storage.Notification) (storage.Notification, error)
}

// MetricTitle — подписи метрик снимков для сообщений.
var MetricTitle = map[string]string{
	"cpu_percent":   "загрузка CPU, %",
	"io_latency_ms": "задержка IO, мс",
	"iops":          "IOPS",
}

// Evaluate проверяет все активные правила один раз.
func Evaluate(store Store, now time.Time) {
	rules, probeNames, err := store.ListRules()
	if err != nil {
		log.Printf("алертинг: не удалось прочитать правила: %v", err)
		return
	}
	for _, r := range rules {
		if r.Status != "active" {
			continue
		}
		if err := evaluateRule(store, r, probeNames[r.ID], now); err != nil {
			log.Printf("алертинг: правило %d «%s»: %v", r.ID, r.Name, err)
		}
	}
}

// evaluateRule проверяет одно правило и открывает/закрывает инцидент.
func evaluateRule(store Store, r storage.Rule, probeName string, now time.Time) error {
	values, err := lastValues(store, r)
	if err != nil {
		return err
	}
	if len(values) < r.Intervals {
		return nil // данных пока меньше, чем требует правило
	}

	violates := true
	for _, v := range values {
		if !compare(v, r.Op, r.Threshold) {
			violates = false
			break
		}
	}
	last := values[0]

	open, err := store.OpenIncidentByRule(r.ID)
	hasOpen := err == nil
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}

	switch {
	case violates && !hasOpen:
		id, err := store.OpenIncident(r.ID, now, last)
		if err != nil {
			return err
		}
		_, err = store.AddNotification(storage.Notification{
			At:         now,
			IncidentID: id,
			Message: fmt.Sprintf("Инцидент №%d: правило «%s» — %s: %g %s %g (%d замеров подряд)",
				id, r.Name, sourceTitle(r, probeName), last, r.Op, r.Threshold, r.Intervals),
			Status: "new",
		})
		if err != nil {
			return err
		}
		log.Printf("алертинг: открыт инцидент №%d по правилу «%s»", id, r.Name)
	case violates && hasOpen:
		return store.TouchIncident(open.ID, last)
	case !violates && hasOpen:
		if err := store.CloseIncident(open.ID, now); err != nil {
			return err
		}
		_, err := store.AddNotification(storage.Notification{
			At:         now,
			IncidentID: open.ID,
			Message:    fmt.Sprintf("Инцидент №%d закрыт: %s вернулось в норму (%g)", open.ID, sourceTitle(r, probeName), last),
			Status:     "new",
		})
		if err != nil {
			return err
		}
		log.Printf("алертинг: закрыт инцидент №%d по правилу «%s»", open.ID, r.Name)
	}
	return nil
}

// lastValues возвращает последние замеры источника правила, новые первыми.
func lastValues(store Store, r storage.Rule) ([]float64, error) {
	if r.Source == "probe" {
		samples, err := store.ListProbeSamples(r.ProbeID, r.Intervals)
		if err != nil {
			return nil, err
		}
		out := make([]float64, 0, len(samples))
		for _, s := range samples {
			out = append(out, s.Value)
		}
		return out, nil
	}
	snaps, err := store.ListSnapshots()
	if err != nil {
		return nil, err
	}
	var out []float64
	for _, s := range snaps {
		if len(out) >= r.Intervals {
			break
		}
		switch r.Source {
		case "cpu_percent":
			out = append(out, s.CPUPercent)
		case "io_latency_ms":
			out = append(out, s.IOLatencyMs)
		case "iops":
			out = append(out, s.IOPS)
		}
	}
	return out, nil
}

// compare применяет оператор правила.
func compare(v float64, op string, threshold float64) bool {
	if op == ">" {
		return v > threshold
	}
	return v < threshold
}

// sourceTitle — читаемое имя источника правила.
func sourceTitle(r storage.Rule, probeName string) string {
	if r.Source == "probe" {
		return "пробник «" + probeName + "»"
	}
	return MetricTitle[r.Source]
}
