package system

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	internalssh "github.com/WELIZARY/tgOps/internal/ssh"
)

// Metrics - метрики одного сервера
type Metrics struct {
	ServerName string
	CPU        float64       // % загрузки
	RAMUsed    uint64        // байт
	RAMTotal   uint64        // байт
	DiskUsed   uint64        // байт (корневой раздел)
	DiskTotal  uint64        // байт
	Load1      float64       // средняя нагрузка за 1 мин
	Load5      float64       // за 5 мин
	Load15     float64       // за 15 мин
	Uptime     time.Duration
	Error      error
}

// Collect собирает метрики с сервера через SSH.
// При ошибке подключения возвращает Metrics с заполненным Error.
func Collect(ctx context.Context, c *internalssh.Client, spec internalssh.ServerSpec) *Metrics {
	m := &Metrics{ServerName: spec.Host}

	// CPU: два снимка /proc/stat с паузой. значения кумулятивные с момента
	// загрузки, поэтому считаем дельту между снимками - это мгновенная
	// загрузка, а не средняя за весь аптайм
	out, err := c.Run(ctx, spec, "grep '^cpu ' /proc/stat; sleep 1; grep '^cpu ' /proc/stat")
	if err != nil {
		m.Error = fmt.Errorf("сбор метрик: %w", err)
		return m
	}
	m.CPU = parseCPUPercent(out)

	// RAM из free -b (в байтах)
	if out, err = c.Run(ctx, spec, "free -b | grep '^Mem:'"); err == nil {
		m.RAMTotal, m.RAMUsed = parseRAM(out)
	}

	// Диск: корневой раздел в байтах
	if out, err = c.Run(ctx, spec, "df -B1 / | tail -1"); err == nil {
		m.DiskTotal, m.DiskUsed = parseDisk(out)
	}

	// Load average
	if out, err = c.Run(ctx, spec, "cat /proc/loadavg"); err == nil {
		m.Load1, m.Load5, m.Load15 = parseLoadAvg(out)
	}

	// Uptime
	if out, err = c.Run(ctx, spec, "cat /proc/uptime"); err == nil {
		m.Uptime = parseUptime(out)
	}

	return m
}

// parseCPUPercent вычисляет % загрузки CPU по двум снимкам строки (мгновенная загрузка, а не средняя за весь аптайм)
func parseCPUPercent(out string) float64 {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		// Запасной вариант: один снимок (среднее с момента загрузки)
		if len(lines) == 1 {
			t, idle := cpuTotals(lines[0])
			if t == 0 {
				return 0
			}
			return (1 - idle/t) * 100
		}
		return 0
	}
	t1, i1 := cpuTotals(lines[0])
	t2, i2 := cpuTotals(lines[len(lines)-1])
	dt := t2 - t1
	di := i2 - i1
	if dt <= 0 {
		return 0
	}
	p := (1 - di/dt) * 100
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p
}

func cpuTotals(line string) (total, idle float64) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return 0, 0
	}
	for i, f := range fields[1:] {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			continue
		}
		total += v
		if i == 3 { // idle - 4-е значение после "cpu"
			idle = v
		}
	}
	return total, idle
}

// parseRAM парсит вывод "free -b | grep '^Mem:'".
// Формат: Mem: total used free shared buff/cache available
func parseRAM(line string) (total, used uint64) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return
	}
	total, _ = strconv.ParseUint(fields[1], 10, 64)
	used, _ = strconv.ParseUint(fields[2], 10, 64)
	return
}

// parseDisk парсит вывод "df -B1 / | tail -1".
// Формат: /dev/sda1 size used avail use% /
func parseDisk(line string) (total, used uint64) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return
	}
	total, _ = strconv.ParseUint(fields[1], 10, 64)
	used, _ = strconv.ParseUint(fields[2], 10, 64)
	return
}

// parseLoadAvg парсит вывод /proc/loadavg.
// Формат: 0.45 0.51 0.48 1/234 5678
func parseLoadAvg(line string) (load1, load5, load15 float64) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return
	}
	load1, _ = strconv.ParseFloat(fields[0], 64)
	load5, _ = strconv.ParseFloat(fields[1], 64)
	load15, _ = strconv.ParseFloat(fields[2], 64)
	return
}

// parseUptime парсит вывод /proc/uptime.
// Формат: 86400.12 123456.78 (секунды работы, секунды простоя)
func parseUptime(line string) time.Duration {
	fields := strings.Fields(line)
	if len(fields) < 1 {
		return 0
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return time.Duration(secs) * time.Second
}
