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
	PID  int
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
// Формат: USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND...
func parsePS(out string) []Process {
	lines := strings.Split(out, "\n")
	var procs []Process
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		pid, _ := strconv.Atoi(fields[1])
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)

		cmd := strings.Join(fields[10:], " ")
		if len(cmd) > 35 {
			cmd = cmd[:32] + "..."
		}

		procs = append(procs, Process{
			PID:  pid,
			User: fields[0],
			CPU:  cpu,
			Mem:  mem,
			CMD:  cmd,
		})
	}
	return procs
}

// formatTop форматирует список процессов в моноширинную таблицу
func formatTop(serverName string, procs []Process) string {
	var sb strings.Builder
	sb.WriteString(formatter.Bold(serverName) + " - топ процессов\n\n")

	var table strings.Builder
	fmt.Fprintf(&table, "%-6s %-10s %5s %5s  %s\n", "PID", "USER", "CPU%", "MEM%", "COMMAND")
	table.WriteString(strings.Repeat("-", 52) + "\n")
	for _, p := range procs {
		fmt.Fprintf(&table, "%-6d %-10s %5.1f %5.1f  %s\n",
			p.PID, truncate(p.User, 10), p.CPU, p.Mem, p.CMD)
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
