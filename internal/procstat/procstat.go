// Пакет procstat снимает текущую нагрузку из /proc.
// В контейнере проверки нет прав на загрузку eBPF (CAP_BPF), поэтому
// для снимка используются счётчики ядра из /proc/stat и /proc/diskstats.
package procstat

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Metrics — значения одного снимка.
type Metrics struct {
	CPUPercent  float64
	IOLatencyMs float64
	IOPS        float64
}

type cpuTimes struct{ idle, total uint64 }

type diskTotals struct {
	ios uint64 // завершённых операций чтения и записи
	ms  uint64 // миллисекунд, потраченных на чтение и запись
}

func readCPU() (cpuTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, v := range fields[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			t.total += n
			if i == 3 || i == 4 { // idle и iowait
				t.idle += n
			}
		}
		return t, nil
	}
	return cpuTimes{}, fmt.Errorf("в /proc/stat нет строки cpu")
}

func readDisks() (diskTotals, error) {
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return diskTotals{}, err
	}
	defer f.Close()
	var d diskTotals
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) < 14 {
			continue
		}
		name := x[2]
		// Пропускаем виртуальные устройства, чтобы не считать операции дважды.
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "zram") {
			continue
		}
		u := func(i int) uint64 { n, _ := strconv.ParseUint(x[i], 10, 64); return n }
		d.ios += u(3) + u(7)
		d.ms += u(6) + u(10)
	}
	return d, nil
}

// Measure делает два замера с интервалом и считает нагрузку между ними.
func Measure(interval time.Duration) (Metrics, error) {
	c1, err := readCPU()
	if err != nil {
		return Metrics{}, err
	}
	d1, err := readDisks()
	if err != nil {
		return Metrics{}, err
	}
	time.Sleep(interval)
	c2, err := readCPU()
	if err != nil {
		return Metrics{}, err
	}
	d2, err := readDisks()
	if err != nil {
		return Metrics{}, err
	}

	var m Metrics
	if dt := c2.total - c1.total; dt > 0 {
		m.CPUPercent = round1(100 * float64(dt-(c2.idle-c1.idle)) / float64(dt))
	}
	ios := d2.ios - d1.ios
	m.IOPS = round1(float64(ios) / interval.Seconds())
	if ios > 0 {
		m.IOLatencyMs = round1(float64(d2.ms-d1.ms) / float64(ios))
	}
	return m, nil
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
