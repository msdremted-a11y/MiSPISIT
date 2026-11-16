// worker — фоновые задачи монитора (курсовая):
//   1. авто-снимки нагрузки из /proc по расписанию (SNAPSHOT_INTERVAL_S, по умолчанию 300 с);
//   2. проверка правил алертинга по снимкам и замерам пробников
//      (ALERT_INTERVAL_S, по умолчанию 10 с).
package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/msdremted-a11y/MiSPISIT/internal/alerts"
	"github.com/msdremted-a11y/MiSPISIT/internal/procstat"
	"github.com/msdremted-a11y/MiSPISIT/internal/storage"
)

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
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

	snapEvery := time.Duration(envInt("SNAPSHOT_INTERVAL_S", 300)) * time.Second
	alertEvery := time.Duration(envInt("ALERT_INTERVAL_S", 10)) * time.Second
	log.Printf("worker запущен: снимок каждые %s, проверка правил каждые %s", snapEvery, alertEvery)

	lastSnap := time.Now().Add(-snapEvery) // первый снимок сразу
	tick := time.NewTicker(alertEvery)
	defer tick.Stop()
	for {
		now := time.Now().UTC()
		if now.Sub(lastSnap) >= snapEvery {
			m, err := procstat.Measure(500 * time.Millisecond)
			if err != nil {
				log.Printf("снимок по расписанию: не удалось измерить: %v", err)
			} else {
				sn, err := store.AddSnapshot(storage.Snapshot{
					TakenAt: now, Author: "по расписанию", Source: "proc",
					CPUPercent: m.CPUPercent, IOLatencyMs: m.IOLatencyMs, IOPS: m.IOPS,
				})
				if err != nil {
					log.Printf("снимок по расписанию: не удалось сохранить: %v", err)
				} else {
					log.Printf("снимок по расписанию №%d: CPU %.1f%%, IO %.1f мс, %.0f IOPS",
						sn.ID, sn.CPUPercent, sn.IOLatencyMs, sn.IOPS)
				}
			}
			lastSnap = now
		}
		alerts.Evaluate(store, now)
		<-tick.C
	}
}
