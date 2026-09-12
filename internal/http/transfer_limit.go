package httpapi

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	mediaDownloadWait  = 8 * time.Second
	updateDownloadWait = 20 * time.Second
	videoDownloadWait  = 8 * time.Second
	musicDownloadWait  = 8 * time.Second
)

var (
	mediaTransferRateBytes  int64 = 5 << 20 // 5MB/s for non-video upload/download
	updateTransferRateBytes int64 = 5 << 20 // 5MB/s for app update package download
	videoTransferRateBytes  int64 = 5 << 20 // 5MB/s for video upload/download
	musicTransferRateBytes  int64 = 5 << 20 // 5MB/s for music download/playback
)

// 下载并发限制：将更新文件与聊天媒体拆分，避免相互阻塞。
// 媒体下载槽位更多，更新下载槽位更小。
var (
	transferSemMu           sync.RWMutex
	mediaDownloadSemaphore  = make(chan struct{}, 12)
	updateDownloadSemaphore = make(chan struct{}, 4)
	videoDownloadSemaphore  = make(chan struct{}, 2)
	musicDownloadSemaphore  = make(chan struct{}, 8)
	uploadSemaphore         = make(chan struct{}, 10)
	videoUploadSemaphore    = make(chan struct{}, 10)
)

func setTransferRateLimits(media, update, video, music int64) {
	if media >= 0 {
		atomic.StoreInt64(&mediaTransferRateBytes, media)
	}
	if update >= 0 {
		atomic.StoreInt64(&updateTransferRateBytes, update)
	}
	if video >= 0 {
		atomic.StoreInt64(&videoTransferRateBytes, video)
	}
	if music >= 0 {
		atomic.StoreInt64(&musicTransferRateBytes, music)
	}
}

func getTransferRateLimits() (media int64, update int64, video int64, music int64) {
	media = atomic.LoadInt64(&mediaTransferRateBytes)
	update = atomic.LoadInt64(&updateTransferRateBytes)
	video = atomic.LoadInt64(&videoTransferRateBytes)
	music = atomic.LoadInt64(&musicTransferRateBytes)
	return
}

func setTransferConcurrencyLimits(media, update, video, music int) {
	transferSemMu.Lock()
	defer transferSemMu.Unlock()
	if media > 0 {
		mediaDownloadSemaphore = make(chan struct{}, clampTransferConcurrency(media))
	}
	if update > 0 {
		updateDownloadSemaphore = make(chan struct{}, clampTransferConcurrency(update))
	}
	if video > 0 {
		videoDownloadSemaphore = make(chan struct{}, clampTransferConcurrency(video))
	}
	if music > 0 {
		musicDownloadSemaphore = make(chan struct{}, clampTransferConcurrency(music))
	}
}

func getTransferConcurrencyLimits() (media int, update int, video int, music int) {
	transferSemMu.RLock()
	defer transferSemMu.RUnlock()
	media = cap(mediaDownloadSemaphore)
	update = cap(updateDownloadSemaphore)
	video = cap(videoDownloadSemaphore)
	music = cap(musicDownloadSemaphore)
	return
}

func clampTransferConcurrency(value int) int {
	if value < 1 {
		return 1
	}
	if value > 200 {
		return 200
	}
	return value
}

func mediaDownloadSem() chan struct{} {
	transferSemMu.RLock()
	defer transferSemMu.RUnlock()
	return mediaDownloadSemaphore
}

func updateDownloadSem() chan struct{} {
	transferSemMu.RLock()
	defer transferSemMu.RUnlock()
	return updateDownloadSemaphore
}

func videoDownloadSem() chan struct{} {
	transferSemMu.RLock()
	defer transferSemMu.RUnlock()
	return videoDownloadSemaphore
}

func musicDownloadSem() chan struct{} {
	transferSemMu.RLock()
	defer transferSemMu.RUnlock()
	return musicDownloadSemaphore
}

func mediaTransferRate() int64 {
	return atomic.LoadInt64(&mediaTransferRateBytes)
}

func videoTransferRate() int64 {
	return atomic.LoadInt64(&videoTransferRateBytes)
}

func limitUploadBody(r *http.Request) {
	mediaRate := atomic.LoadInt64(&mediaTransferRateBytes)
	if r == nil || r.Body == nil || mediaRate <= 0 {
		return
	}
	r.Body = newThrottledReadCloser(r.Body, mediaRate)
}

func withDownloadLimit(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rate := atomic.LoadInt64(&mediaTransferRateBytes)
		sem := mediaDownloadSem()
		if !acquireSemaphore(r, sem, mediaDownloadWait) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer func() { <-sem }()
		if rate <= 0 {
			h.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(newThrottledResponseWriter(w, rate), r)
	})
}

func withUpdateDownloadLimit(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rate := atomic.LoadInt64(&updateTransferRateBytes)
		sem := updateDownloadSem()
		if !acquireSemaphore(r, sem, updateDownloadWait) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer func() { <-sem }()
		if rate <= 0 {
			h.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(newThrottledResponseWriter(w, rate), r)
	})
}

func withVideoDownloadLimit(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rate := atomic.LoadInt64(&videoTransferRateBytes)
		sem := videoDownloadSem()
		if !acquireSemaphore(r, sem, videoDownloadWait) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer func() { <-sem }()
		if rate <= 0 {
			h.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(newThrottledResponseWriter(w, rate), r)
	})
}

func withMusicDownloadLimit(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rate := atomic.LoadInt64(&musicTransferRateBytes)
		sem := musicDownloadSem()
		if !acquireSemaphore(r, sem, musicDownloadWait) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer func() { <-sem }()
		if rate <= 0 {
			h.ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(newThrottledResponseWriter(w, rate), r)
	})
}

func acquireSemaphore(r *http.Request, sem chan struct{}, wait time.Duration) bool {
	if sem == nil {
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case sem <- struct{}{}:
		return true
	case <-r.Context().Done():
		return false
	case <-timer.C:
		return false
	}
}

func withMediaDownloadLimit(videoEnabled func() bool, h http.Handler) http.Handler {
	general := withDownloadLimit(h)
	video := withVideoDownloadLimit(h)
	music := withMusicDownloadLimit(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMusicPath(r.URL.Path) {
			music.ServeHTTP(w, r)
			return
		}
		if isVideoPath(r.URL.Path) {
			if videoEnabled != nil && !videoEnabled() {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("video disabled"))
				return
			}
			video.ServeHTTP(w, r)
			return
		}
		general.ServeHTTP(w, r)
	})
}

func withStaticMediaCache(h http.Handler) http.Handler {
	if h == nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
		w.Header().Set("Expires", time.Now().UTC().Add(7*24*time.Hour).Format(http.TimeFormat))
		h.ServeHTTP(w, r)
	})
}

func acquireUploadSlot(isVideo bool) func() {
	if isVideo {
		videoUploadSemaphore <- struct{}{}
		return func() { <-videoUploadSemaphore }
	}
	uploadSemaphore <- struct{}{}
	return func() { <-uploadSemaphore }
}

func isVideoPath(path string) bool {
	p := strings.ToLower(path)
	return strings.HasSuffix(p, ".mp4")
}

func isMusicPath(path string) bool {
	p := strings.ToLower(path)
	return strings.HasSuffix(p, ".mp3") ||
		strings.HasSuffix(p, ".m4a") ||
		strings.HasSuffix(p, ".aac") ||
		strings.HasSuffix(p, ".wav") ||
		strings.HasSuffix(p, ".wave") ||
		strings.HasSuffix(p, ".ogg") ||
		strings.HasSuffix(p, ".flac")
}

type throttledReader struct {
	r     io.Reader
	rate  int64
	start time.Time
	read  int64
}

func newThrottledReader(r io.Reader, rate int64) io.Reader {
	if rate <= 0 {
		return r
	}
	return &throttledReader{r: r, rate: rate}
}

func (t *throttledReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n <= 0 {
		return n, err
	}
	if t.start.IsZero() {
		t.start = time.Now()
	}
	t.read += int64(n)
	expected := time.Duration(t.read * int64(time.Second) / t.rate)
	elapsed := time.Since(t.start)
	if expected > elapsed {
		time.Sleep(expected - elapsed)
	}
	return n, err
}

type throttledReadCloser struct {
	io.Reader
	io.Closer
}

func newThrottledReadCloser(rc io.ReadCloser, rate int64) io.ReadCloser {
	if rate <= 0 {
		return rc
	}
	return &throttledReadCloser{Reader: newThrottledReader(rc, rate), Closer: rc}
}

type throttledResponseWriter struct {
	http.ResponseWriter
	rate    int64
	start   time.Time
	written int64
}

func newThrottledResponseWriter(w http.ResponseWriter, rate int64) http.ResponseWriter {
	if rate <= 0 {
		return w
	}
	return &throttledResponseWriter{ResponseWriter: w, rate: rate}
}

func (w *throttledResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if n <= 0 {
		return n, err
	}
	if w.start.IsZero() {
		w.start = time.Now()
	}
	w.written += int64(n)
	expected := time.Duration(w.written * int64(time.Second) / w.rate)
	elapsed := time.Since(w.start)
	if expected > elapsed {
		time.Sleep(expected - elapsed)
	}
	return n, err
}

func (w *throttledResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *throttledResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (w *throttledResponseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}
