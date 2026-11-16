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
	SnapshotID int    // какой снимок вызвал уведомление (0 — не снимок)
	IncidentID int    // какой инцидент вызвал уведомление (0 — не инцидент)
	Message    string // что произошло
	Status     string // "new" — ещё не прочитано, "read" — прочитано
}

// Probe — пробник eBPF, собранный в конструкторе (курсовая).
// Пользователь задаёт смысловые параметры, система сама генерирует
// скрипт (строку предметного языка) и код bpftrace.
type Probe struct {
	ID        int
	Name      string // имя пробника, которое дал пользователь
	Event     string // syscall_read, disk_io, file_open, net_connect
	Process   string // фильтр по имени процесса (comm), пусто = все процессы
	Agg       string // count, bytes, avg_latency_ms
	IntervalS int    // период снятия замера, секунды
	DSL       string // скрипт (строка предметного языка), сгенерированный из параметров
	Bpftrace  string // сгенерированный скрипт bpftrace
	Status    string // active — работает, paused — на паузе
	CreatedAt time.Time
}

// ProbeSample — один замер работающего пробника.
type ProbeSample struct {
	ID      int
	ProbeID int
	At      time.Time
	Value   float64
}

// Rule — правило алертинга (курсовая): если метрика снимка или замер
// пробника N интервалов подряд за порогом, открывается инцидент.
type Rule struct {
	ID        int
	Name      string
	Source    string // cpu_percent, io_latency_ms, iops — метрика снимка; probe — замер пробника
	ProbeID   int    // какой пробник, когда Source = "probe"
	Op        string // ">" или "<"
	Threshold float64
	Intervals int    // сколько замеров подряд должны нарушить порог
	Status    string // active, paused
	CreatedAt time.Time
}

// Incident — открытый правилом инцидент.
type Incident struct {
	ID        int
	RuleID    int
	RuleName  string // заполняется при выборке списка
	OpenedAt  time.Time
	ClosedAt  *time.Time
	Status    string // open, ack (подтверждён), closed
	LastValue float64
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
CREATE INDEX IF NOT EXISTS notification_at_idx ON notification (at DESC);

CREATE TABLE IF NOT EXISTS probe (
    id         SERIAL PRIMARY KEY,
    name       TEXT        NOT NULL,
    event      TEXT        NOT NULL CHECK (event IN ('syscall_read', 'disk_io', 'file_open', 'net_connect')),
    process    TEXT        NOT NULL DEFAULT '',
    agg        TEXT        NOT NULL CHECK (agg IN ('count', 'bytes', 'avg_latency_ms')),
    interval_s INTEGER     NOT NULL CHECK (interval_s BETWEEN 1 AND 3600),
    dsl        TEXT        NOT NULL,
    bpftrace   TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS probe_sample (
    id       SERIAL PRIMARY KEY,
    probe_id INTEGER        NOT NULL REFERENCES probe (id) ON DELETE CASCADE,
    at       TIMESTAMPTZ    NOT NULL DEFAULT now(),
    value    DOUBLE PRECISION NOT NULL
);
CREATE INDEX IF NOT EXISTS probe_sample_probe_at_idx ON probe_sample (probe_id, at DESC);

CREATE TABLE IF NOT EXISTS rule (
    id         SERIAL PRIMARY KEY,
    name       TEXT             NOT NULL,
    source     TEXT             NOT NULL CHECK (source IN ('cpu_percent', 'io_latency_ms', 'iops', 'probe')),
    probe_id   INTEGER          REFERENCES probe (id) ON DELETE CASCADE,
    op         TEXT             NOT NULL CHECK (op IN ('>', '<')),
    threshold  DOUBLE PRECISION NOT NULL,
    intervals  INTEGER          NOT NULL DEFAULT 1 CHECK (intervals BETWEEN 1 AND 100),
    status     TEXT             NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
    created_at TIMESTAMPTZ      NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS incident (
    id         SERIAL PRIMARY KEY,
    rule_id    INTEGER          NOT NULL REFERENCES rule (id) ON DELETE CASCADE,
    opened_at  TIMESTAMPTZ      NOT NULL DEFAULT now(),
    closed_at  TIMESTAMPTZ,
    status     TEXT             NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'ack', 'closed')),
    last_value DOUBLE PRECISION
);
CREATE INDEX IF NOT EXISTS incident_opened_idx ON incident (opened_at DESC);

-- Связка уведомлений с инцидентами (курсовая): уведомление может
-- относиться не только к снимку, но и к инциденту.
ALTER TABLE notification ALTER COLUMN snapshot_id DROP NOT NULL;
ALTER TABLE notification ADD COLUMN IF NOT EXISTS incident_id INTEGER REFERENCES incident (id) ON DELETE CASCADE;`

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
	var snap, inc any
	if n.SnapshotID > 0 {
		snap = n.SnapshotID
	}
	if n.IncidentID > 0 {
		inc = n.IncidentID
	}
	err := p.db.QueryRow(
		`INSERT INTO notification (at, snapshot_id, incident_id, message, status)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		n.At, snap, inc, n.Message, n.Status,
	).Scan(&n.ID)
	return n, err
}

// ListNotifications возвращает последние уведомления, новые сверху.
func (p *Postgres) ListNotifications() ([]Notification, error) {
	rows, err := p.db.Query(
		`SELECT id, at, COALESCE(snapshot_id, 0), COALESCE(incident_id, 0), message, status
		 FROM notification ORDER BY at DESC, id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.At, &n.SnapshotID, &n.IncidentID, &n.Message, &n.Status); err != nil {
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

// AddProbe сохраняет пробник и возвращает его с номером, который выдала база.
func (p *Postgres) AddProbe(pr Probe) (Probe, error) {
	if pr.Status == "" {
		pr.Status = "active"
	}
	err := p.db.QueryRow(
		`INSERT INTO probe (name, event, process, agg, interval_s, dsl, bpftrace, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		pr.Name, pr.Event, pr.Process, pr.Agg, pr.IntervalS, pr.DSL, pr.Bpftrace, pr.Status,
	).Scan(&pr.ID, &pr.CreatedAt)
	return pr, err
}

// ListProbes возвращает все пробники и последний замер каждого (0, если замеров нет).
func (p *Postgres) ListProbes() ([]Probe, map[int]float64, map[int]bool, error) {
	rows, err := p.db.Query(
		`SELECT id, name, event, process, agg, interval_s, dsl, bpftrace, status, created_at
		 FROM probe ORDER BY id`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	var out []Probe
	for rows.Next() {
		var pr Probe
		if err := rows.Scan(&pr.ID, &pr.Name, &pr.Event, &pr.Process, &pr.Agg,
			&pr.IntervalS, &pr.DSL, &pr.Bpftrace, &pr.Status, &pr.CreatedAt); err != nil {
			return nil, nil, nil, err
		}
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	last := map[int]float64{}
	has := map[int]bool{}
	srows, err := p.db.Query(
		`SELECT DISTINCT ON (probe_id) probe_id, value
		 FROM probe_sample ORDER BY probe_id, at DESC`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var id int
		var v float64
		if err := srows.Scan(&id, &v); err != nil {
			return nil, nil, nil, err
		}
		last[id] = v
		has[id] = true
	}
	return out, last, has, srows.Err()
}

// GetProbe возвращает один пробник по номеру.
func (p *Postgres) GetProbe(id int) (Probe, error) {
	var pr Probe
	err := p.db.QueryRow(
		`SELECT id, name, event, process, agg, interval_s, dsl, bpftrace, status, created_at
		 FROM probe WHERE id = $1`, id,
	).Scan(&pr.ID, &pr.Name, &pr.Event, &pr.Process, &pr.Agg,
		&pr.IntervalS, &pr.DSL, &pr.Bpftrace, &pr.Status, &pr.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return pr, ErrNotFound
	}
	return pr, err
}

// SetProbeStatus переводит пробник в состояние active/paused.
func (p *Postgres) SetProbeStatus(id int, status string) error {
	res, err := p.db.Exec(`UPDATE probe SET status = $1 WHERE id = $2`, status, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProbe удаляет пробник вместе с замерами (ON DELETE CASCADE).
func (p *Postgres) DeleteProbe(id int) error {
	_, err := p.db.Exec(`DELETE FROM probe WHERE id = $1`, id)
	return err
}

// AddProbeSample записывает замер пробника.
func (p *Postgres) AddProbeSample(s ProbeSample) error {
	_, err := p.db.Exec(
		`INSERT INTO probe_sample (probe_id, at, value) VALUES ($1, $2, $3)`,
		s.ProbeID, s.At, s.Value)
	return err
}

// ListProbeSamples возвращает последние замеры пробника, новые сверху.
func (p *Postgres) ListProbeSamples(probeID, limit int) ([]ProbeSample, error) {
	rows, err := p.db.Query(
		`SELECT id, probe_id, at, value FROM probe_sample
		 WHERE probe_id = $1 ORDER BY at DESC, id DESC LIMIT $2`, probeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProbeSample
	for rows.Next() {
		var s ProbeSample
		if err := rows.Scan(&s.ID, &s.ProbeID, &s.At, &s.Value); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AddRule сохраняет правило алертинга.
func (p *Postgres) AddRule(r Rule) (Rule, error) {
	if r.Status == "" {
		r.Status = "active"
	}
	var probeID any
	if r.Source == "probe" && r.ProbeID > 0 {
		probeID = r.ProbeID
	}
	err := p.db.QueryRow(
		`INSERT INTO rule (name, source, probe_id, op, threshold, intervals, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		r.Name, r.Source, probeID, r.Op, r.Threshold, r.Intervals, r.Status,
	).Scan(&r.ID, &r.CreatedAt)
	return r, err
}

// ListRules возвращает все правила с именами связанных пробников.
func (p *Postgres) ListRules() ([]Rule, map[int]string, error) {
	rows, err := p.db.Query(
		`SELECT r.id, r.name, r.source, COALESCE(r.probe_id, 0), r.op, r.threshold,
		        r.intervals, r.status, r.created_at, COALESCE(p.name, '')
		 FROM rule r LEFT JOIN probe p ON p.id = r.probe_id ORDER BY r.id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Rule
	probeNames := map[int]string{}
	for rows.Next() {
		var r Rule
		var pname string
		if err := rows.Scan(&r.ID, &r.Name, &r.Source, &r.ProbeID, &r.Op, &r.Threshold,
			&r.Intervals, &r.Status, &r.CreatedAt, &pname); err != nil {
			return nil, nil, err
		}
		if r.ProbeID > 0 {
			probeNames[r.ID] = pname
		}
		out = append(out, r)
	}
	return out, probeNames, rows.Err()
}

// SetRuleStatus переводит правило в состояние active/paused.
func (p *Postgres) SetRuleStatus(id int, status string) error {
	_, err := p.db.Exec(`UPDATE rule SET status = $1 WHERE id = $2`, status, id)
	return err
}

// DeleteRule удаляет правило вместе с инцидентами (ON DELETE CASCADE).
func (p *Postgres) DeleteRule(id int) error {
	_, err := p.db.Exec(`DELETE FROM rule WHERE id = $1`, id)
	return err
}

// OpenIncident открывает инцидент по правилу и возвращает его номер.
func (p *Postgres) OpenIncident(ruleID int, at time.Time, value float64) (int, error) {
	var id int
	err := p.db.QueryRow(
		`INSERT INTO incident (rule_id, opened_at, last_value) VALUES ($1, $2, $3) RETURNING id`,
		ruleID, at, value,
	).Scan(&id)
	return id, err
}

// OpenIncidentByRule возвращает открытый (не закрытый) инцидент правила, если он есть.
func (p *Postgres) OpenIncidentByRule(ruleID int) (Incident, error) {
	var in Incident
	err := p.db.QueryRow(
		`SELECT id, rule_id, opened_at, closed_at, status, COALESCE(last_value, 0)
		 FROM incident WHERE rule_id = $1 AND status <> 'closed'
		 ORDER BY opened_at DESC LIMIT 1`, ruleID,
	).Scan(&in.ID, &in.RuleID, &in.OpenedAt, &in.ClosedAt, &in.Status, &in.LastValue)
	if errors.Is(err, sql.ErrNoRows) {
		return in, ErrNotFound
	}
	return in, err
}

// TouchIncident обновляет последнее значение открытого инцидента.
func (p *Postgres) TouchIncident(id int, value float64) error {
	_, err := p.db.Exec(`UPDATE incident SET last_value = $1 WHERE id = $2`, value, id)
	return err
}

// CloseIncident закрывает инцидент (условие правила перестало выполняться).
func (p *Postgres) CloseIncident(id int, at time.Time) error {
	_, err := p.db.Exec(`UPDATE incident SET status = 'closed', closed_at = $1 WHERE id = $2`, at, id)
	return err
}

// AckIncident переводит инцидент в «подтверждён».
func (p *Postgres) AckIncident(id int) error {
	_, err := p.db.Exec(`UPDATE incident SET status = 'ack' WHERE id = $1 AND status = 'open'`, id)
	return err
}

// ListIncidents возвращает последние инциденты с именами правил, новые сверху.
func (p *Postgres) ListIncidents(limit int) ([]Incident, error) {
	rows, err := p.db.Query(
		`SELECT i.id, i.rule_id, r.name, i.opened_at, i.closed_at, i.status, COALESCE(i.last_value, 0)
		 FROM incident i JOIN rule r ON r.id = i.rule_id
		 ORDER BY i.opened_at DESC, i.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var in Incident
		if err := rows.Scan(&in.ID, &in.RuleID, &in.RuleName, &in.OpenedAt, &in.ClosedAt, &in.Status, &in.LastValue); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// CountOpenIncidents возвращает число незакрытых инцидентов.
func (p *Postgres) CountOpenIncidents() (int, error) {
	var n int
	err := p.db.QueryRow(`SELECT count(*) FROM incident WHERE status <> 'closed'`).Scan(&n)
	return n, err
}

// ProbeStats — статистика замеров пробника за период.
type ProbeStats struct {
	ProbeID int
	Count   int
	Min     float64
	Max     float64
	Avg     float64
}

// ProbeStatsSince возвращает статистику замеров каждого пробника за период.
func (p *Postgres) ProbeStatsSince(since time.Time) ([]ProbeStats, error) {
	rows, err := p.db.Query(
		`SELECT probe_id, count(*), min(value), max(value), avg(value)
		 FROM probe_sample WHERE at >= $1 GROUP BY probe_id ORDER BY probe_id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProbeStats
	for rows.Next() {
		var s ProbeStats
		if err := rows.Scan(&s.ProbeID, &s.Count, &s.Min, &s.Max, &s.Avg); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SnapshotStats — статистика снимков за период.
type SnapshotStats struct {
	Count      int
	AvgCPU     float64
	MaxCPU     float64
	AvgLatency float64
	MaxLatency float64
	AvgIOPS    float64
	MaxIOPS    float64
}

// SnapshotStatsSince возвращает статистику снимков за период.
func (p *Postgres) SnapshotStatsSince(since time.Time) (SnapshotStats, error) {
	var s SnapshotStats
	err := p.db.QueryRow(
		`SELECT count(*), COALESCE(avg(cpu_percent),0), COALESCE(max(cpu_percent),0),
		        COALESCE(avg(io_latency_ms),0), COALESCE(max(io_latency_ms),0),
		        COALESCE(avg(iops),0), COALESCE(max(iops),0)
		 FROM snapshot WHERE taken_at >= $1`, since,
	).Scan(&s.Count, &s.AvgCPU, &s.MaxCPU, &s.AvgLatency, &s.MaxLatency, &s.AvgIOPS, &s.MaxIOPS)
	return s, err
}

// IncidentStats — сводка по инцидентам за период.
type IncidentStats struct {
	Opened int
	Open   int
	Closed int
}

// IncidentStatsSince возвращает сводку по инцидентам за период.
func (p *Postgres) IncidentStatsSince(since time.Time) (IncidentStats, error) {
	var s IncidentStats
	err := p.db.QueryRow(
		`SELECT count(*),
		        count(*) FILTER (WHERE status <> 'closed'),
		        count(*) FILTER (WHERE status = 'closed')
		 FROM incident WHERE opened_at >= $1`, since,
	).Scan(&s.Opened, &s.Open, &s.Closed)
	return s, err
}

// ListIncidentsSince возвращает инциденты за период (для отчёта).
func (p *Postgres) ListIncidentsSince(since time.Time) ([]Incident, error) {
	rows, err := p.db.Query(
		`SELECT i.id, i.rule_id, r.name, i.opened_at, i.closed_at, i.status, COALESCE(i.last_value, 0)
		 FROM incident i JOIN rule r ON r.id = i.rule_id
		 WHERE i.opened_at >= $1 ORDER BY i.opened_at DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var in Incident
		if err := rows.Scan(&in.ID, &in.RuleID, &in.RuleName, &in.OpenedAt, &in.ClosedAt, &in.Status, &in.LastValue); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
