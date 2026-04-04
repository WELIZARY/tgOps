package system

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/WELIZARY/tgOps/internal/formatter"
	internalssh "github.com/WELIZARY/tgOps/internal/ssh"
)

// Process - один процесс из топ-листа
type Process struct {
	User string
	CPU  float64
	Mem  float64
	CMD  string
}

// CollectTop собирает топ-10 процессов по CPU и форматирует для Telegram
func CollectTop(ctx context.Context, c *internalssh.Client, spec internalssh.ServerSpec, serverName string) (string, error) {
	out, err := c.Run(ctx, spec, "ps aux --sort=-%cpu | head -11")
	if err != nil {
		return "", fmt.Errorf("ps aux: %w", err)
	}
	procs := parsePS(out)
	return formatTop(serverName, procs), nil
}

// parsePS парсит вывод ps aux (первая строка - заголовок, пропускаем).
// формат: USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND...
func parsePS(out string) []Process {
	lines := strings.Split(out, "\n")
	var procs []Process
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)

		// пропускаем сам процесс ps
		if strings.HasPrefix(fields[10], "ps") {
			continue
		}

		// показываем только имя бинарника без пути и аргументов
		name := fields[10]
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		procs = append(procs, Process{
			User: fields[0],
			CPU:  cpu,
			Mem:  mem,
			CMD:  name,
		})
	}
	return procs
}

// formatTop форматирует список процессов в моноширинную таблицу для Telegram
func formatTop(serverName string, procs []Process) string {
	var sb strings.Builder
	sb.WriteString(formatter.Bold(serverName) + " — топ процессов\n\n")

	var table strings.Builder
	fmt.Fprintf(&table, "%5s  %5s  %-8s  %s\n", "CPU%", "MEM%", "USER", "PROCESS")
	table.WriteString(strings.Repeat("─", 38) + "\n")
	for _, p := range procs {
		fmt.Fprintf(&table, "%5.1f  %5.1f  %-8s  %s\n",
			p.CPU, p.Mem, truncate(p.User, 8), p.CMD)
	}

	sb.WriteString(formatter.Pre(table.String()))
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "."
}
