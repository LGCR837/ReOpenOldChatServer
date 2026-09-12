package httpapi

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	logLineLimit     = 400
	chatLineLimit    = 200
	requestWindowCap = 200
)

var defaultMonitor = newMonitor()

func StartTUI() {
	defaultMonitor.Start()
}

func StopTUI() {
	defaultMonitor.Stop()
}

func recordRequest(d time.Duration) {
	defaultMonitor.RecordRequest(d)
}

func recordStatus(status int) {
	defaultMonitor.RecordStatus(status)
}

func chatLogf(format string, args ...interface{}) {
	defaultMonitor.Chatf(format, args...)
}

type monitor struct {
	logMu     sync.Mutex
	logLines  []string
	chatMu    sync.Mutex
	chatLines []string

	reqMu        sync.Mutex
	reqDurations []time.Duration
	reqIndex     int
	reqCount     int
	reqSum       time.Duration
	reqTotal     uint64
	req4xx       uint64
	req5xx       uint64

	plainWriter io.Writer
	writer      io.Writer

	tuiActive int32

	lastCPUTime  time.Duration
	lastCPUCheck time.Time
	lastCPUUsage float64

	startTime    time.Time
	lastReqTotal uint64
	lastReqCheck time.Time
	lastRPS      float64

	stopOnce sync.Once
	stopCh   chan struct{}
}

func newMonitor() *monitor {
	return &monitor{
		plainWriter:  os.Stdout,
		reqDurations: make([]time.Duration, requestWindowCap),
		startTime:    time.Now(),
		stopCh:       make(chan struct{}),
	}
}

func (m *monitor) Start() {
	if !canStartTUI() {
		return
	}
	if atomic.LoadInt32(&m.tuiActive) == 1 {
		return
	}
	if !m.startTUI() {
		return
	}
	log.SetOutput(m.LogWriter())
}

func (m *monitor) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	m.stopTUI()
}

func (m *monitor) LogWriter() io.Writer {
	if m.writer != nil {
		return m.writer
	}
	m.writer = monitorWriter{m: m}
	return m.writer
}

func (m *monitor) RecordRequest(d time.Duration) {
	m.reqMu.Lock()
	if m.reqCount < len(m.reqDurations) {
		m.reqDurations[m.reqIndex] = d
		m.reqSum += d
		m.reqCount++
	} else {
		m.reqSum -= m.reqDurations[m.reqIndex]
		m.reqDurations[m.reqIndex] = d
		m.reqSum += d
	}
	m.reqIndex++
	if m.reqIndex >= len(m.reqDurations) {
		m.reqIndex = 0
	}
	atomic.AddUint64(&m.reqTotal, 1)
	m.reqMu.Unlock()
}

func (m *monitor) RecordStatus(status int) {
	if status >= 500 {
		atomic.AddUint64(&m.req5xx, 1)
		return
	}
	if status >= 400 {
		atomic.AddUint64(&m.req4xx, 1)
	}
}

func (m *monitor) AvgRequestDuration() time.Duration {
	m.reqMu.Lock()
	defer m.reqMu.Unlock()
	if m.reqCount == 0 {
		return 0
	}
	return time.Duration(int64(m.reqSum) / int64(m.reqCount))
}

func (m *monitor) TotalRequests() uint64 {
	return atomic.LoadUint64(&m.reqTotal)
}

func (m *monitor) requestRate() float64 {
	now := time.Now()
	total := m.TotalRequests()
	if m.lastReqCheck.IsZero() {
		m.lastReqCheck = now
		m.lastReqTotal = total
		return 0
	}
	delta := total - m.lastReqTotal
	elapsed := now.Sub(m.lastReqCheck).Seconds()
	if elapsed <= 0 {
		return m.lastRPS
	}
	rps := float64(delta) / elapsed
	m.lastReqTotal = total
	m.lastReqCheck = now
	m.lastRPS = rps
	return rps
}

type requestStats struct {
	count int
	avg   time.Duration
	p95   time.Duration
	max   time.Duration
}

func (m *monitor) requestStats() requestStats {
	m.reqMu.Lock()
	count := m.reqCount
	if count == 0 {
		m.reqMu.Unlock()
		return requestStats{}
	}
	avg := time.Duration(int64(m.reqSum) / int64(count))
	durations := make([]time.Duration, count)
	if count < len(m.reqDurations) {
		copy(durations, m.reqDurations[:count])
	} else {
		copy(durations, m.reqDurations[m.reqIndex:])
		copy(durations[len(m.reqDurations)-m.reqIndex:], m.reqDurations[:m.reqIndex])
	}
	m.reqMu.Unlock()
	max := durations[0]
	for _, d := range durations {
		if d > max {
			max = d
		}
	}
	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})
	p95Index := int(math.Ceil(0.95*float64(count))) - 1
	if p95Index < 0 {
		p95Index = 0
	}
	if p95Index >= count {
		p95Index = count - 1
	}
	return requestStats{
		count: count,
		avg:   avg,
		p95:   durations[p95Index],
		max:   max,
	}
}

func (m *monitor) LogLine(line string) {
	if line == "" {
		return
	}
	m.logMu.Lock()
	m.logLines = append(m.logLines, line)
	if len(m.logLines) > logLineLimit {
		m.logLines = m.logLines[len(m.logLines)-logLineLimit:]
	}
	m.logMu.Unlock()
}

func (m *monitor) copyLogLines() []string {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	out := make([]string, len(m.logLines))
	copy(out, m.logLines)
	return out
}

func (m *monitor) copyChatLines() []string {
	m.chatMu.Lock()
	defer m.chatMu.Unlock()
	out := make([]string, len(m.chatLines))
	copy(out, m.chatLines)
	return out
}

func (m *monitor) Chatf(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	m.chatMu.Lock()
	m.chatLines = append(m.chatLines, line)
	if len(m.chatLines) > chatLineLimit {
		m.chatLines = m.chatLines[len(m.chatLines)-chatLineLimit:]
	}
	m.chatMu.Unlock()
}

func (m *monitor) formatStats() []string {
	mem := &runtime.MemStats{}
	runtime.ReadMemStats(mem)
	cpuUsage := m.readCPUUsage()
	stats := m.requestStats()
	rps := m.requestRate()
	reqTotal := m.TotalRequests()
	req4xx := atomic.LoadUint64(&m.req4xx)
	req5xx := atomic.LoadUint64(&m.req5xx)
	lines := []string{
		fmt.Sprintf("Uptime: %s", formatUptime(time.Since(m.startTime))),
		fmt.Sprintf("Req/s: %.2f", rps),
		fmt.Sprintf("Avg RT: %s", formatDuration(stats.avg)),
		fmt.Sprintf("P95 RT: %s", formatDuration(stats.p95)),
		fmt.Sprintf("Max RT: %s", formatDuration(stats.max)),
		fmt.Sprintf("CPU: %.1f%%", cpuUsage),
		fmt.Sprintf("Goroutines: %d", runtime.NumGoroutine()),
		fmt.Sprintf("Heap: %s", formatBytes(int(mem.HeapAlloc))),
		fmt.Sprintf("Sys: %s", formatBytes(int(mem.Sys))),
		fmt.Sprintf("Requests: %d", reqTotal),
		fmt.Sprintf("4xx/5xx: %d/%d", req4xx, req5xx),
	}
	return lines
}

func (m *monitor) readCPUUsage() float64 {
	now := time.Now()
	cpu := readCPUTime()
	if m.lastCPUCheck.IsZero() {
		m.lastCPUCheck = now
		m.lastCPUTime = cpu
		return 0
	}
	deltaCPU := cpu - m.lastCPUTime
	deltaWall := now.Sub(m.lastCPUCheck)
	if deltaWall <= 0 {
		return m.lastCPUUsage
	}
	usage := float64(deltaCPU) / float64(deltaWall) / float64(runtime.NumCPU()) * 100
	if usage < 0 {
		usage = 0
	}
	m.lastCPUCheck = now
	m.lastCPUTime = cpu
	m.lastCPUUsage = usage
	return usage
}

type monitorWriter struct {
	m *monitor
}

func (w monitorWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	parts := strings.Split(string(p), "\n")
	for _, line := range parts {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			w.m.LogLine(line)
		}
	}
	if atomic.LoadInt32(&w.m.tuiActive) == 0 && w.m.plainWriter != nil {
		_, _ = w.m.plainWriter.Write(p)
	}
	return len(p), nil
}

func formatChatPreview(msgType, body string) string {
	text := strings.TrimSpace(body)
	switch msgType {
	case "image":
		text = "[image]"
	case "voice":
		text = "[voice]"
	case "video":
		text = "[video]"
	case "resource":
		text = "[resource]"
	case "red_packet":
		text = "[red_packet]"
	}
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.TrimSpace(text)
	const limit = 60
	if len(text) > limit {
		text = text[:limit-3] + "..."
	}
	return text
}

func formatUptime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%02ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%02dm", hours, mins)
}
