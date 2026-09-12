package httpapi

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func canStartTUI() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func (m *monitor) startTUI() bool {
	if atomic.LoadInt32(&m.tuiActive) == 1 {
		return true
	}
	if !canStartTUI() {
		return false
	}
	atomic.StoreInt32(&m.tuiActive, 1)
	go m.loopTUI()
	return true
}

func (m *monitor) stopTUI() {
	atomic.StoreInt32(&m.tuiActive, 0)
}

func (m *monitor) loopTUI() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			m.stopTUI()
			return
		case <-ticker.C:
			if atomic.LoadInt32(&m.tuiActive) == 0 {
				return
			}
			m.renderTUI()
		}
	}
}

func (m *monitor) renderTUI() {
	width, height := terminalSize()
	if width < 40 {
		width = 40
	}
	if height < 12 {
		height = 12
	}
	topH := int(float64(height) * 0.67)
	if topH < 5 {
		topH = 5
	}
	if topH > height-3 {
		topH = height - 3
	}
	separatorH := 1
	bottomH := height - topH - separatorH
	if bottomH < 3 {
		bottomH = 3
		topH = height - bottomH - separatorH
	}

	logLines := m.copyLogLines()
	chatLines := m.copyChatLines()
	stats := m.formatStats()
	now := time.Now()

	logArea := buildLogArea(logLines, width, topH, now)
	bottomArea := buildBottomArea(stats, chatLines, width, bottomH)

	var builder strings.Builder
	builder.WriteString("\033[H")
	builder.WriteString(logArea)
	builder.WriteString(strings.Repeat("-", width))
	builder.WriteString("\n")
	builder.WriteString(bottomArea)

	writer := bufio.NewWriter(m.plainWriter)
	_, _ = writer.WriteString(builder.String())
	_ = writer.Flush()
}

func buildLogArea(lines []string, width, height int, now time.Time) string {
	if height <= 0 {
		return ""
	}
	var builder strings.Builder
	header := fmt.Sprintf("Logs (%d) %s", len(lines), now.Format("15:04:05"))
	title := padRight(truncateRunes(header, width), width)
	builder.WriteString(title)
	builder.WriteString("\n")
	available := height - 1
	if available <= 0 {
		return builder.String()
	}
	tail := tailLines(lines, available)
	blankCount := available - len(tail)
	for i := 0; i < blankCount; i++ {
		builder.WriteString(padRight("", width))
		builder.WriteString("\n")
	}
	for _, line := range tail {
		builder.WriteString(padRight(truncateRunes(cleanLine(line), width), width))
		builder.WriteString("\n")
	}
	return builder.String()
}

func buildBottomArea(stats []string, chat []string, width, height int) string {
	if height <= 0 {
		return ""
	}
	leftW := width / 2
	rightW := width - leftW - 1
	if rightW < 10 {
		rightW = width - leftW - 1
	}
	var builder strings.Builder
	headerLeft := padRight(truncateRunes("Stats (avg/p95/max)", leftW), leftW)
	headerRight := padRight(truncateRunes(fmt.Sprintf("Chat (%d)", len(chat)), rightW), rightW)
	builder.WriteString(headerLeft)
	builder.WriteString("|")
	builder.WriteString(headerRight)
	builder.WriteString("\n")
	available := height - 1
	if available <= 0 {
		return builder.String()
	}
	leftLines := make([]string, available)
	for i := 0; i < available; i++ {
		if i < len(stats) {
			leftLines[i] = stats[i]
		} else {
			leftLines[i] = ""
		}
	}
	rightTail := tailLines(chat, available)
	rightLines := make([]string, available)
	blankCount := available - len(rightTail)
	for i := 0; i < blankCount; i++ {
		rightLines[i] = ""
	}
	for i, line := range rightTail {
		rightLines[blankCount+i] = line
	}
	for i := 0; i < available; i++ {
		builder.WriteString(padRight(truncateRunes(cleanLine(leftLines[i]), leftW), leftW))
		builder.WriteString("|")
		builder.WriteString(padRight(truncateRunes(cleanLine(rightLines[i]), rightW), rightW))
		builder.WriteString("\n")
	}
	return builder.String()
}

func tailLines(lines []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	return string(runes) + strings.Repeat(" ", width-len(runes))
}

func truncateRunes(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func cleanLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}
