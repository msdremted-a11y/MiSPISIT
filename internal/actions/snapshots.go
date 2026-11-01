// Пакет actions — три главные функции ЛР2:
//  1. сохранить снимок нагрузки (изменение данных);
//  2. показать снимки и карточку снимка (просмотр);
//  3. отклонить снимок без метрики или с недопустимым значением (отказ).
package actions

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/msdremted-a11y/MiSPISIT/internal/procstat"
	"github.com/msdremted-a11y/MiSPISIT/internal/storage"
)

// Store — то, что функциям нужно от хранилища. В ЛР2 это JSON, в ЛР3 будет PostgreSQL.
type Store interface {
	AddSnapshot(storage.Snapshot) (storage.Snapshot, error)
	ListSnapshots() []storage.Snapshot
	GetSnapshot(id int) (storage.Snapshot, error)
	AddRejection(storage.Rejection) (storage.Rejection, error)
	ListRejections() []storage.Rejection
}

// Input — данные формы /snapshots/new.
type Input struct {
	Mode        string // "proc" — снять из /proc, "manual" — сохранить введённые значения
	Author      string
	CPUPercent  string
	IOLatencyMs string
	IOPS        string
}

// Service объединяет три функции.
type Service struct {
	Store   Store
	Measure func() (procstat.Metrics, error)
	Now     func() time.Time
}

// Create — функция 1. Возвращает сохранённый снимок или отказ (функция 3).
func (s *Service) Create(in Input) (storage.Snapshot, *storage.Rejection, error) {
	author := strings.TrimSpace(in.Author)
	sn := storage.Snapshot{TakenAt: s.Now().UTC(), Author: author, Source: "manual"}

	if in.Mode == "proc" {
		m, err := s.Measure()
		if err != nil {
			return s.reject(author, "снять снимок из /proc", "не удалось прочитать /proc: "+err.Error())
		}
		sn.Source = "proc"
		sn.CPUPercent, sn.IOLatencyMs, sn.IOPS = m.CPUPercent, m.IOLatencyMs, m.IOPS
	}

	attempt := describe(in)
	if author == "" {
		return s.reject("не указан", attempt, "не указано, кто снимает снимок")
	}

	if in.Mode != "proc" {
		var reason string
		if sn.CPUPercent, reason = parseMetric(in.CPUPercent, "загрузка CPU", 0, 100, "%"); reason != "" {
			return s.reject(author, attempt, reason)
		}
		if sn.IOLatencyMs, reason = parseMetric(in.IOLatencyMs, "задержка IO", 0, 10000, "мс"); reason != "" {
			return s.reject(author, attempt, reason)
		}
		if sn.IOPS, reason = parseMetric(in.IOPS, "IOPS", 0, 1000000, "оп/с"); reason != "" {
			return s.reject(author, attempt, reason)
		}
	}

	saved, err := s.Store.AddSnapshot(sn)
	return saved, nil, err
}

// List — функция 2: последние снимки.
func (s *Service) List() []storage.Snapshot { return s.Store.ListSnapshots() }

// Get — функция 2: карточка одного снимка.
func (s *Service) Get(id int) (storage.Snapshot, error) { return s.Store.GetSnapshot(id) }

// Rejections — функция 3: журнал отказов.
func (s *Service) Rejections() []storage.Rejection { return s.Store.ListRejections() }

func (s *Service) reject(author, attempt, reason string) (storage.Snapshot, *storage.Rejection, error) {
	r, err := s.Store.AddRejection(storage.Rejection{
		At: s.Now().UTC(), Author: author, Attempt: attempt, Reason: reason,
	})
	return storage.Snapshot{}, &r, err
}

// parseMetric проверяет одно значение: оно должно быть, быть числом и попадать в диапазон.
func parseMetric(raw, name string, min, max float64, unit string) (float64, string) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if raw == "" {
		return 0, fmt.Sprintf("снимок без метрики: не указано значение «%s»", name)
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Sprintf("«%s» = «%s» — это не число", name, raw)
	}
	if v < min || v > max {
		return 0, fmt.Sprintf("«%s» = %g %s вне допустимого диапазона %g–%g %s", name, v, unit, min, max, unit)
	}
	return v, ""
}

func describe(in Input) string {
	if in.Mode == "proc" {
		return "снять снимок из /proc"
	}
	show := func(v string) string {
		if strings.TrimSpace(v) == "" {
			return "пусто"
		}
		return strings.TrimSpace(v)
	}
	return fmt.Sprintf("сохранить снимок вручную: CPU = %s %%, задержка IO = %s мс, IOPS = %s",
		show(in.CPUPercent), show(in.IOLatencyMs), show(in.IOPS))
}
