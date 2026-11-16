// probe-runner — исполнитель eBPF-пробников (курсовая).
// Работает в привилегированном контейнере с bpftrace: забирает из базы
// активные пробники, запускает для каждого процесс bpftrace и пишет
// замеры (строки "VALUE <число>") обратно в базу.
package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/msdremted-a11y/MiSPISIT/internal/storage"
)

// running — один запущенный пробник.
type running struct {
	cancel context.CancelFunc
	script string // чтобы перезапускать, если скрипт изменился
}

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("не задана переменная DATABASE_URL (адрес базы PostgreSQL)")
	}
	store, err := storage.OpenPostgres(dbURL, 30)
	if err != nil {
		log.Fatalf("не удалось подключиться к базе: %v", err)
	}
	if _, err := exec.LookPath("bpftrace"); err != nil {
		log.Fatalf("bpftrace не найден в образе: %v", err)
	}
	// bpftrace ищет трасспоинты в tracefs; в контейнере она не смонтирована.
	// Монтируем сами (контейнер привилегированный); ошибки не страшны:
	// если ФС уже смонтирована, mount просто вернёт код ошибки.
	for _, m := range [][2]string{
		{"tracefs", "/sys/kernel/tracing"},
		{"debugfs", "/sys/kernel/debug"},
	} {
		if out, err := exec.Command("mount", "-t", m[0], m[0], m[1]).CombinedOutput(); err != nil {
			log.Printf("mount %s: %v (%s) — если события ядра видны, это не мешает", m[1], err, strings.TrimSpace(string(out)))
		}
	}

	sup := &supervisor{store: store, procs: map[int]running{}}
	log.Printf("probe-runner запущен, опрос базы каждые 5 с")
	for {
		sup.sync()
		time.Sleep(5 * time.Second)
	}
}

type supervisor struct {
	store *storage.Postgres
	mu    sync.Mutex
	procs map[int]running
}

// sync сверяет активные пробники в базе с запущенными процессами.
func (s *supervisor) sync() {
	probes, _, _, err := s.store.ListProbes()
	if err != nil {
		log.Printf("не удалось прочитать пробники: %v", err)
		return
	}
	want := map[int]storage.Probe{}
	for _, p := range probes {
		if p.Status == "active" {
			want[p.ID] = p
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// остановить удалённые и поставленные на паузу
	for id, r := range s.procs {
		p, ok := want[id]
		if !ok || p.Bpftrace != r.script {
			log.Printf("пробник %d: останавливаю", id)
			r.cancel()
			delete(s.procs, id)
		}
	}
	// запустить новые
	for id, p := range want {
		if _, ok := s.procs[id]; ok {
			continue
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.procs[id] = running{cancel: cancel, script: p.Bpftrace}
		go runProbe(ctx, s.store, p)
		log.Printf("пробник %d «%s»: запущен (интервал %d с)", id, p.Name, p.IntervalS)
	}
}

// runProbe запускает bpftrace и читает замеры; при падении — перезапуск с паузой.
func runProbe(ctx context.Context, store *storage.Postgres, p storage.Probe) {
	for {
		if ctx.Err() != nil {
			return
		}
		err := runOnce(ctx, store, p)
		if ctx.Err() != nil {
			return
		}
		log.Printf("пробник %d «%s»: bpftrace завершился (%v), перезапуск через 5 с", p.ID, p.Name, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// runOnce — один сеанс bpftrace: от запуска до завершения.
func runOnce(ctx context.Context, store *storage.Postgres, p storage.Probe) error {
	cmd := exec.CommandContext(ctx, "bpftrace", "-e", p.Bpftrace)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = newLogWriter(p.ID)
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for sc.Scan() {
		v, ok := parseSample(sc.Text(), p.Agg)
		if !ok {
			continue
		}
		if err := store.AddProbeSample(storage.ProbeSample{ProbeID: p.ID, At: time.Now().UTC(), Value: v}); err != nil {
			log.Printf("пробник %d: не удалось записать замер: %v", p.ID, err)
		}
	}
	return cmd.Wait()
}

// scalarRe — строка вида «@c: 484» (count, sum).
var scalarRe = regexp.MustCompile(`^@\w+:\s+([0-9]+(?:\.[0-9]+)?)\s*$`)

// statsRe — строка вида «@lat: count 2378, average 1106920, total 2632257083».
var statsRe = regexp.MustCompile(`^@\w+:\s+count \d+, average ([0-9]+(?:\.[0-9]+)?), total \d+\s*$`)

// parseSample извлекает число из строки вывода bpftrace.
// Для avg_latency_ms значение average приходит в наносекундах — переводим в мс.
func parseSample(line, agg string) (float64, bool) {
	line = strings.TrimSpace(line)
	if m := statsRe.FindStringSubmatch(line); m != nil {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		if agg == "avg_latency_ms" {
			f = f / 1e6
		}
		return f, true
	}
	if m := scalarRe.FindStringSubmatch(line); m != nil {
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// logWriter перекладывает stderr bpftrace в журнал контейнера.
type logWriter struct{ probeID int }

func newLogWriter(id int) *logWriter { return &logWriter{probeID: id} }

func (w *logWriter) Write(b []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if line != "" {
			log.Printf("пробник %d bpftrace: %s", w.probeID, line)
		}
	}
	return len(b), nil
}
