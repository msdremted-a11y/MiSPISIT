// Пакет storage — постоянное хранение данных в PostgreSQL (ЛР3).
// Структура базы создаётся самим приложением при запуске.
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq" // драйвер PostgreSQL для database/sql
)

// Snapshot — снимок нагрузки системы.
type Snapshot struct {
	ID          int
	TakenAt     time.Time
	Author      string  // кто снял снимок
	Source      string  // "proc" — снят автоматически из /proc, "manual" — введён вручную
	CPUPercent  float64 // загрузка CPU, %
	IOLatencyMs float64 // средняя задержка ввода-вывода, мс
	IOPS        float64 // операций ввода-вывода в секунду
}

// Rejection — отклонённая попытка сохранить снимок.
type Rejection struct {
	ID      int
	At      time.Time
	Author  string // кто пытался
	Attempt string // что пытались сохранить
	Reason  string // почему система не приняла
}

// Notification — уведомление о сохранённом снимке (ЛР5).
// Живёт в отдельной таблице: это событие, а не поле старой записи.
type Notification struct {
	ID         int
	At         time.Time
	SnapshotID int    // какая запись вызвала уведомление
	Message    string // что произошло
	Status     string // "new" — ещё не прочитано, "read" — прочитано
}

// ErrNotFound — записи с таким номером нет.
var ErrNotFound = errors.New("запись не найдена")

const schema = `
CREATE TABLE IF NOT EXISTS snapshot (
    id            SERIAL PRIMARY KEY,
    taken_at      TIMESTAMPTZ      NOT NULL DEFAULT now(),
    author        TEXT             NOT NULL,
    source        TEXT             NOT NULL CHECK (source IN ('proc', 'manual')),
    cpu_percent   DOUBLE PRECISION NOT NULL CHECK (cpu_percent BETWEEN 0 AND 100),
    io_latency_ms DOUBLE PRECISION NOT NULL CHECK (io_latency_ms >= 0),
    iops          DOUBLE PRECISION NOT NULL CHECK (iops >= 0)
);
CREATE INDEX IF NOT EXISTS snapshot_taken_at_idx ON snapshot (taken_at DESC);

CREATE TABLE IF NOT EXISTS rejection (
    id      SERIAL PRIMARY KEY,
    at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    author  TEXT        NOT NULL,
    attempt TEXT        NOT NULL,
    reason  TEXT        NOT NULL
);

CREATE TABLE IF NOT EXISTS notification (
    id          SERIAL PRIMARY KEY,
    at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    snapshot_id INTEGER     NOT NULL REFERENCES snapshot (id),
    message     TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'read'))
);
CREATE INDEX IF NOT EXISTS notification_at_idx ON notification (at DESC);`

// Postgres хранит снимки и отказы в PostgreSQL.
type Postgres struct{ db *sql.DB }

// OpenPostgres подключается к базе, повторяя попытки, пока база запускается,
// и создаёт таблицы, если их ещё нет.
func OpenPostgres(url string, attempts int) (*Postgres, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	for i := 1; ; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		if i >= attempts {
			return nil, fmt.Errorf("база не ответила за %d попыток: %w", attempts, err)
		}
		log.Printf("база ещё не готова (попытка %d из %d): %v", i, attempts, err)
		time.Sleep(2 * time.Second)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("не удалось создать таблицы: %w", err)
	}
	return &Postgres{db: db}, nil
}

// AddSnapshot сохраняет снимок и возвращает его с номером, который выдала база.
func (p *Postgres) AddSnapshot(s Snapshot) (Snapshot, error) {
	err := p.db.QueryRow(
		`INSERT INTO snapshot (taken_at, author, source, cpu_percent, io_latency_ms, iops)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		s.TakenAt, s.Author, s.Source, s.CPUPercent, s.IOLatencyMs, s.IOPS,
	).Scan(&s.ID)
	return s, err
}

// ListSnapshots возвращает последние снимки, новые сверху.
func (p *Postgres) ListSnapshots() ([]Snapshot, error) {
	rows, err := p.db.Query(
		`SELECT id, taken_at, author, source, cpu_percent, io_latency_ms, iops
		 FROM snapshot ORDER BY taken_at DESC, id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var s Snapshot
		if err := rows.Scan(&s.ID, &s.TakenAt, &s.Author, &s.Source, &s.CPUPercent, &s.IOLatencyMs, &s.IOPS); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSnapshot возвращает один снимок по номеру.
func (p *Postgres) GetSnapshot(id int) (Snapshot, error) {
	var s Snapshot
	err := p.db.QueryRow(
		`SELECT id, taken_at, author, source, cpu_percent, io_latency_ms, iops
		 FROM snapshot WHERE id = $1`, id,
	).Scan(&s.ID, &s.TakenAt, &s.Author, &s.Source, &s.CPUPercent, &s.IOLatencyMs, &s.IOPS)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// CountSnapshots возвращает число сохранённых снимков.
func (p *Postgres) CountSnapshots() (int, error) {
	var n int
	err := p.db.QueryRow(`SELECT count(*) FROM snapshot`).Scan(&n)
	return n, err
}

// AddRejection записывает отклонённую попытку.
func (p *Postgres) AddRejection(r Rejection) (Rejection, error) {
	err := p.db.QueryRow(
		`INSERT INTO rejection (at, author, attempt, reason) VALUES ($1, $2, $3, $4) RETURNING id`,
		r.At, r.Author, r.Attempt, r.Reason,
	).Scan(&r.ID)
	return r, err
}

// ListRejections возвращает последние отказы, новые сверху.
func (p *Postgres) ListRejections() ([]Rejection, error) {
	rows, err := p.db.Query(
		`SELECT id, at, author, attempt, reason FROM rejection ORDER BY at DESC, id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rejection
	for rows.Next() {
		var r Rejection
		if err := rows.Scan(&r.ID, &r.At, &r.Author, &r.Attempt, &r.Reason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountRejections возвращает число отказов.
func (p *Postgres) CountRejections() (int, error) {
	var n int
	err := p.db.QueryRow(`SELECT count(*) FROM rejection`).Scan(&n)
	return n, err
}

// AddNotification записывает уведомление о событии и возвращает его с номером.
func (p *Postgres) AddNotification(n Notification) (Notification, error) {
	if n.Status == "" {
		n.Status = "new"
	}
	err := p.db.QueryRow(
		`INSERT INTO notification (at, snapshot_id, message, status)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		n.At, n.SnapshotID, n.Message, n.Status,
	).Scan(&n.ID)
	return n, err
}

// ListNotifications возвращает последние уведомления, новые сверху.
func (p *Postgres) ListNotifications() ([]Notification, error) {
	rows, err := p.db.Query(
		`SELECT id, at, snapshot_id, message, status
		 FROM notification ORDER BY at DESC, id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.At, &n.SnapshotID, &n.Message, &n.Status); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkNotificationRead переводит одно уведомление в статус «прочитано».
func (p *Postgres) MarkNotificationRead(id int) error {
	res, err := p.db.Exec(`UPDATE notification SET status = 'read' WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkAllNotificationsRead переводит все новые уведомления в «прочитано».
func (p *Postgres) MarkAllNotificationsRead() error {
	_, err := p.db.Exec(`UPDATE notification SET status = 'read' WHERE status = 'new'`)
	return err
}

// CountUnreadNotifications возвращает число непрочитанных уведомлений.
func (p *Postgres) CountUnreadNotifications() (int, error) {
	var n int
	err := p.db.QueryRow(`SELECT count(*) FROM notification WHERE status = 'new'`).Scan(&n)
	return n, err
}
