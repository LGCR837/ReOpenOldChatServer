package httpapi

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

func (a *API) loadVoiceMessageAudioFromHTTP(mediaURL, relativePath string) ([]byte, string, string, error) {
	candidates := a.buildVoiceMediaFetchCandidates(mediaURL, relativePath)
	if len(candidates) == 0 {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusNotFound,
			Code:    "media_not_found",
			Message: "media not found",
		}
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var lastErr error
	for i := 0; i < len(candidates); i++ {
		targetURL := candidates[i]
		req, err := http.NewRequest(http.MethodGet, targetURL, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = voiceTranscribeHTTPError{
				Status:  http.StatusNotFound,
				Code:    "media_not_found",
				Message: "media not found",
			}
			resp.Body.Close()
			continue
		}
		audioData, err := io.ReadAll(io.LimitReader(resp.Body, maxVoiceASRUploadBytes+1))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if len(audioData) == 0 {
			lastErr = voiceTranscribeHTTPError{
				Status:  http.StatusUnprocessableEntity,
				Code:    "empty_audio",
				Message: "empty audio",
			}
			continue
		}
		if int64(len(audioData)) > maxVoiceASRUploadBytes {
			lastErr = voiceTranscribeHTTPError{
				Status:  http.StatusRequestEntityTooLarge,
				Code:    "audio_too_large",
				Message: "audio too large",
			}
			continue
		}
		fileName := filepath.Base(relativePath)
		if fileName == "" || fileName == "." {
			fileName = "voice.3gp"
		}
		contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
		if contentType == "" {
			contentType = mime.TypeByExtension(filepath.Ext(fileName))
		}
		if contentType == "" {
			contentType = "audio/3gpp"
		}
		return audioData, fileName, contentType, nil
	}
	if lastErr == nil {
		lastErr = voiceTranscribeHTTPError{
			Status:  http.StatusNotFound,
			Code:    "media_not_found",
			Message: "media not found",
		}
	}
	return nil, "", "", lastErr
}

func (a *API) buildVoiceMediaFetchCandidates(mediaURL, relativePath string) []string {
	seen := map[string]struct{}{}
	candidates := make([]string, 0, 6)
	add := func(raw string) {
		target := strings.TrimSpace(raw)
		if target == "" {
			return
		}
		if _, ok := seen[target]; ok {
			return
		}
		seen[target] = struct{}{}
		candidates = append(candidates, target)
	}

	if isSafeAbsoluteUploadURL(mediaURL) {
		add(mediaURL)
	}

	rel := strings.TrimLeft(strings.TrimSpace(relativePath), "/")
	if rel == "" {
		return candidates
	}
	if publicBase, err := a.resolveServerPublicBaseURL(); err == nil && strings.TrimSpace(publicBase) != "" {
		base := strings.TrimRight(publicBase, "/")
		add(base + "/v1/uploads/" + rel)
		add(base + "/uploads/" + rel)
	}
	dataBase := strings.TrimRight(strings.TrimSpace(a.cfg.DataServerBaseURL), "/")
	if dataBase != "" {
		add(dataBase + "/v1/uploads/" + rel)
		add(dataBase + "/uploads/" + rel)
	}
	return candidates
}

func isSafeAbsoluteUploadURL(raw string) bool {
	target := strings.TrimSpace(raw)
	if target == "" {
		return false
	}
	u, err := url.Parse(target)
	if err != nil || u == nil {
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "http" && scheme != "https" {
		return false
	}
	path := strings.ToLower(strings.TrimSpace(u.Path))
	if !strings.Contains(path, "/uploads/") {
		return false
	}
	return true
}
