package httpapi

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type gzipResponseWriter struct {
	http.ResponseWriter
	writer             *gzip.Writer
	status             int
	headerWritten      bool
	disableCompression bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.headerWritten {
		return
	}
	w.status = code
	w.headerWritten = true
	if !canGzipResponse(code, w.Header()) {
		w.disableCompression = true
		w.ResponseWriter.WriteHeader(code)
		return
	}
	addVaryAcceptEncoding(w.Header())
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Del("Content-Length")
	writer, err := gzip.NewWriterLevel(w.ResponseWriter, gzip.BestSpeed)
	if err != nil {
		w.disableCompression = true
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.writer = writer
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	if w.disableCompression || w.writer == nil {
		return w.ResponseWriter.Write(b)
	}
	return w.writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if w.writer != nil {
		_ = w.writer.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *gzipResponseWriter) Close() error {
	if w.writer == nil {
		return nil
	}
	err := w.writer.Close()
	w.writer = nil
	return err
}

func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.writer != nil {
		_ = w.writer.Close()
		w.writer = nil
	}
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *gzipResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func gzipResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldGzipRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		addVaryAcceptEncoding(w.Header())
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer func() {
			_ = gw.Close()
		}()
		next.ServeHTTP(gw, r)
	})
}

func shouldGzipRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(r.Method, http.MethodHead) {
		return false
	}
	if r.Header.Get("Range") != "" {
		return false
	}
	if isWebSocketUpgrade(r) {
		return false
	}
	if shouldSkipGzipPath(r.URL.Path) {
		return false
	}
	ae := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Encoding")))
	if ae == "" || strings.Index(ae, "gzip") < 0 {
		return false
	}
	if strings.Index(ae, "gzip;q=0") >= 0 {
		return false
	}
	return true
}

func isWebSocketUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return true
	}
	connection := strings.ToLower(r.Header.Get("Connection"))
	return strings.Index(connection, "upgrade") >= 0
}

func shouldSkipGzipPath(path string) bool {
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)
	return strings.HasPrefix(lower, "/uploads/") ||
		strings.HasPrefix(lower, "/v1/uploads/") ||
		strings.HasPrefix(lower, "/update/") ||
		strings.HasPrefix(lower, "/landing-assets/") ||
		strings.HasPrefix(lower, "/v1/music/cover/") ||
		strings.HasPrefix(lower, "/music/cover/")
}

func canGzipResponse(status int, header http.Header) bool {
	if status < 200 || status == http.StatusNoContent || status == http.StatusNotModified {
		return false
	}
	if header == nil {
		return true
	}
	if encoding := strings.TrimSpace(header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return false
	}
	if header.Get("Content-Range") != "" {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(header.Get("Content-Type")))
	if contentType == "" {
		return true
	}
	if strings.HasPrefix(contentType, "text/") {
		return true
	}
	if strings.Index(contentType, "json") >= 0 || strings.Index(contentType, "javascript") >= 0 || strings.Index(contentType, "xml") >= 0 || strings.Index(contentType, "svg") >= 0 || strings.Index(contentType, "form-urlencoded") >= 0 {
		return true
	}
	return false
}

func addVaryAcceptEncoding(header http.Header) {
	if header == nil {
		return
	}
	vary := header.Get("Vary")
	if vary == "" {
		header.Set("Vary", "Accept-Encoding")
		return
	}
	parts := strings.Split(vary, ",")
	for i := range parts {
		if strings.EqualFold(strings.TrimSpace(parts[i]), "Accept-Encoding") {
			return
		}
	}
	header.Set("Vary", vary+", Accept-Encoding")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusRecorder) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		duration := time.Since(start)
		recordRequest(duration)
		recordStatus(rec.status)
		log.Printf("%s | %3d | %-4s | %-36s | %6s | %8s | %s | %s",
			time.Now().Format("15:04:05"),
			rec.status,
			r.Method,
			truncatePath(r.URL.Path, 36),
			formatDuration(duration),
			formatBytes(rec.bytes),
			clientIP(r),
			truncateUserAgent(r.UserAgent(), 24),
		)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				writeError(w, http.StatusInternalServerError, "server_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func formatBytes(size int) string {
	if size < 1024 {
		return fmt.Sprintf("%dB", size)
	}
	kb := float64(size) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1fKB", kb)
	}
	mb := kb / 1024
	return fmt.Sprintf("%.1fMB", mb)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < 10*time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func truncatePath(path string, max int) string {
	if max <= 0 || len(path) <= max {
		return path
	}
	if max < 4 {
		return path[:max]
	}
	return path[:max-3] + "..."
}

// cacheStaticAssets wraps an http.Handler and sets Cache-Control header
// for static assets. Font files (.woff2, .woff, .ttf, .eot) get permanent
// cache (1 year, immutable) since they rarely change. Other assets get
// short cache to allow updates.
func cacheStaticAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Font files: cache permanently
		if isFontFile(r.URL.Path) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Other assets: short cache (1 hour)
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		next.ServeHTTP(w, r)
	})
}

func isFontFile(path string) bool {
	// Check common font file extensions
	if len(path) < 5 {
		return false
	}
	ext := strings.ToLower(path[len(path)-5:])
	switch ext {
	case ".woff2", ".woff", ".ttf", ".eot":
		return true
	}
	// Also check .otf (4 chars)
	if len(path) >= 4 {
		ext4 := strings.ToLower(path[len(path)-4:])
		if ext4 == ".otf" {
			return true
		}
	}
	return false
}

func truncateUserAgent(ua string, max int) string {
	ua = strings.ReplaceAll(ua, "\n", " ")
	ua = strings.ReplaceAll(ua, "\r", " ")
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "-"
	}
	if max <= 0 || len(ua) <= max {
		return ua
	}
	if max < 4 {
		return ua[:max]
	}
	return ua[:max-3] + "..."
}
