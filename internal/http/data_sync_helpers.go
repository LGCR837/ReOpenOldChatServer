package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var errDataServerPushEndpointNotFound = errors.New("data_server_push_endpoint_not_found")

type dataServerPushResponse struct {
	OK     bool   `json:"ok"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	Error  string `json:"error"`
}

func syncLogf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	log.Printf("[SYNC] %s", msg)
	chatLogf("[SYNC] %s", msg)
}

type dataServerPullRequest struct {
	SourceURL string `json:"source_url"`
	SaveAs    string `json:"save_as"`
	SHA256    string `json:"sha256,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

func (a *API) shouldSyncToDataServer() bool {
	if a == nil {
		return false
	}
	return normalizedDataServerBaseURL(a.cfg.DataServerBaseURL) != ""
}

func (a *API) maybePushUploadToDataServer(uploadURL string) {
	if err := a.pushUploadToDataServer(uploadURL); err != nil {
		syncLogf("failed url=%s err=%v", strings.TrimSpace(uploadURL), err)
	}
}

func (a *API) pushUploadToDataServer(uploadURL string) error {
	if !a.shouldSyncToDataServer() {
		return nil
	}
	relPath, ok := uploadURLToRelativePath(uploadURL)
	if !ok {
		return nil
	}
	return a.pushRelativeUploadToDataServer(relPath)
}

func uploadURLToRelativePath(uploadURL string) (string, bool) {
	raw := strings.TrimSpace(uploadURL)
	if raw == "" {
		return "", false
	}
	raw = strings.SplitN(raw, "?", 2)[0]
	idx := strings.Index(strings.ToLower(raw), "/v1/uploads/")
	if idx >= 0 {
		raw = raw[idx+len("/v1/uploads/"):]
	} else {
		idx = strings.Index(strings.ToLower(raw), "/uploads/")
		if idx < 0 {
			return "", false
		}
		raw = raw[idx+len("/uploads/"):]
	}
	raw = strings.Trim(raw, " ")
	raw = strings.ReplaceAll(raw, "\\", "/")
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(raw))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", false
	}
	return clean, true
}

func (a *API) pushRelativeUploadToDataServer(relPath string) error {
	if a == nil {
		return nil
	}
	if strings.TrimSpace(relPath) == "" {
		return nil
	}

	absPath := filepath.Join(a.cfg.UploadDir, filepath.FromSlash(relPath))
	shaText, fileSize, hashErr := fileSHA256(absPath)
	if hashErr != nil {
		return fmt.Errorf("hash file failed: %w", hashErr)
	}

	var lastErr error
	for i := 0; i < 2; i++ {
		err := a.postDataServerPush(relPath, absPath, shaText, fileSize)
		if err == nil {
			syncLogf("push ok path=%s bytes=%d", relPath, fileSize)
			return nil
		}
		if errors.Is(err, errDataServerPushEndpointNotFound) {
			syncLogf("push endpoint missing; fallback pull path=%s", relPath)
			if fallbackErr := a.postDataServerPull(relPath, shaText, fileSize); fallbackErr == nil {
				syncLogf("fallback pull ok path=%s bytes=%d", relPath, fileSize)
				return nil
			} else {
				lastErr = fmt.Errorf("push 404 and fallback pull failed: %w", fallbackErr)
			}
		} else {
			lastErr = err
		}
		if i == 0 {
			time.Sleep(220 * time.Millisecond)
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unknown sync error")
	}
	return lastErr
}

func (a *API) postDataServerPush(relPath, absPath, shaText string, fileSize int64) error {
	if a == nil {
		return fmt.Errorf("nil api")
	}
	base := normalizedDataServerBaseURL(a.cfg.DataServerBaseURL)
	if base == "" {
		return fmt.Errorf("data server disabled")
	}
	file, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer file.Close()

	pushURL := base + "/internal/push?save_as=" + url.QueryEscape(relPath)
	syncLogf("push start target=%s path=%s size=%d", pushURL, relPath, fileSize)
	req, err := http.NewRequest(http.MethodPost, pushURL, file)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-File-Size", strconv.FormatInt(fileSize, 10))
	if strings.TrimSpace(shaText) != "" {
		req.Header.Set("X-File-SHA256", strings.TrimSpace(shaText))
	}
	if token := strings.TrimSpace(a.cfg.DataServerSyncToken); token != "" {
		req.Header.Set("X-Sync-Token", token)
	}

	client := a.dataSyncClient
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", errDataServerPushEndpointNotFound, pushURL)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if len(respBody) == 0 {
		return nil
	}
	var out dataServerPushResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil
	}
	if !out.OK {
		if strings.TrimSpace(out.Error) == "" {
			return fmt.Errorf("data server returned not ok")
		}
		return fmt.Errorf(out.Error)
	}
	return nil
}

func (a *API) postDataServerPull(relPath, shaText string, fileSize int64) error {
	if a == nil {
		return fmt.Errorf("nil api")
	}
	base := normalizedDataServerBaseURL(a.cfg.DataServerBaseURL)
	if base == "" {
		return fmt.Errorf("data server disabled")
	}
	publicBase, err := a.resolveServerPublicBaseURL()
	if err != nil {
		return err
	}
	sourceURL := strings.TrimRight(publicBase, "/") + "/v1/uploads/" + strings.TrimLeft(relPath, "/")
	payload := dataServerPullRequest{SourceURL: sourceURL, SaveAs: relPath, SHA256: shaText, Size: fileSize}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	pullURL := base + "/internal/pull"
	syncLogf("pull fallback start target=%s source=%s", pullURL, sourceURL)
	req, err := http.NewRequest(http.MethodPost, pullURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(a.cfg.DataServerSyncToken); token != "" {
		req.Header.Set("X-Sync-Token", token)
	}

	client := a.dataSyncClient
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pull http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func (a *API) resolveServerPublicBaseURL() (string, error) {
	if a == nil {
		return "", fmt.Errorf("nil api")
	}
	if base := strings.TrimRight(strings.TrimSpace(a.cfg.PublicBaseURL), "/"); base != "" {
		return base, nil
	}
	port := strings.TrimSpace(a.cfg.Port)
	if port == "" {
		port = "8080"
	}
	return "http://127.0.0.1:" + port, nil
}

func normalizedDataServerBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return ""
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Host) == "" {
		return strings.TrimRight(base, "/")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if parsed.Port() == "" {
		host := strings.TrimSpace(parsed.Hostname())
		if host != "" {
			parsed.Host = net.JoinHostPort(host, "9090")
		}
	}
	lowerPath := strings.ToLower(parsed.EscapedPath())
	for _, suffix := range []string{"/internal/push", "/internal/pull", "/v1/uploads", "/uploads", "/files"} {
		if strings.HasSuffix(lowerPath, suffix) {
			parsed.Path = parsed.Path[:len(parsed.Path)-len(suffix)]
			lowerPath = strings.ToLower(parsed.EscapedPath())
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	return strings.TrimRight(parsed.String(), "/")
}

func fileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
