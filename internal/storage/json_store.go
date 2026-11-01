// Пакет storage — временное хранение данных ЛР2 в JSON-файле.
// В ЛР3 этот файл заменится на хранилище PostgreSQL с теми же методами.
package storage

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"
)

// Snapshot — снимок нагрузки системы.
type Snapshot struct {
	ID          int       `json:"id"`
	TakenAt     time.Time `json:"taken_at"`
	Author      string    `json:"author"`        // кто снял снимок
	Source      string    `json:"source"`        // "proc" — снят автоматически из /proc, "manual" — введён вручную
	CPUPercent  float64   `json:"cpu_percent"`   // загрузка CPU, %
	IOLatencyMs float64   `json:"io_latency_ms"` // средняя задержка ввода-вывода, мс
	IOPS        float64   `json:"iops"`          // операций ввода-вывода в секунду
}

// Rejection — отклонённая попытка сохранить снимок.
type Rejection struct {
	ID      int       `json:"id"`
	At      time.Time `json:"at"`
	Author  string    `json:"author"`  // кто пытался
	Attempt string    `json:"attempt"` // что пытались сохранить
	Reason  string    `json:"reason"`  // почему система не приняла
}

type fileData struct {
	Snapshots []Snapshot  `json:"snapshots"`
	Rejected  []Rejection `json:"rejected"`
}

// JSONStore хранит снимки и отказы в одном JSON-файле.
type JSONStore struct {
	mu   sync.Mutex
	path string
	data fileData
}

// ErrNotFound — записи с таким номером нет.
var ErrNotFound = errors.New("запись не найдена")

// OpenJSON читает файл с данными. Если файла нет, начинает с пустого набора.
func OpenJSON(path string) (*JSONStore, error) {
	s := &JSONStore{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *JSONStore) save() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// AddSnapshot дописывает снимок в файл и возвращает его с присвоенным номером.
func (s *JSONStore) AddSnapshot(sn Snapshot) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sn.ID = 1
	for _, x := range s.data.Snapshots {
		if x.ID >= sn.ID {
			sn.ID = x.ID + 1
		}
	}
	s.data.Snapshots = append(s.data.Snapshots, sn)
	return sn, s.save()
}

// ListSnapshots возвращает снимки, новые сверху.
func (s *JSONStore) ListSnapshots() []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Snapshot(nil), s.data.Snapshots...)
	sort.Slice(out, func(i, j int) bool { return out[i].TakenAt.After(out[j].TakenAt) })
	return out
}

// GetSnapshot возвращает один снимок по номеру.
func (s *JSONStore) GetSnapshot(id int) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.data.Snapshots {
		if x.ID == id {
			return x, nil
		}
	}
	return Snapshot{}, ErrNotFound
}

// AddRejection записывает отклонённую попытку.
func (s *JSONStore) AddRejection(r Rejection) (Rejection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = 1
	for _, x := range s.data.Rejected {
		if x.ID >= r.ID {
			r.ID = x.ID + 1
		}
	}
	s.data.Rejected = append(s.data.Rejected, r)
	return r, s.save()
}

// ListRejections возвращает отказы, новые сверху.
func (s *JSONStore) ListRejections() []Rejection {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Rejection(nil), s.data.Rejected...)
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}
