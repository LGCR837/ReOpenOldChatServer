package httpapi

import (
	"encoding/json"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const messagePayloadVersion = 1

func isVoiceMessageType(msgType string) bool {
	return strings.EqualFold(strings.TrimSpace(msgType), "voice")
}

func extractVoiceTextFromBody(body string) string {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return ""
	}
	obj := map[string]interface{}{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return ""
	}
	value, ok := obj["voice_text"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func mergeVoiceTextIntoBody(body, text string) (string, error) {
	safeText := strings.TrimSpace(text)
	if safeText == "" {
		return "", voiceTranscribeHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "asr_empty",
			Message: "empty transcription",
		}
	}

	payload := map[string]interface{}{}
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			payload = map[string]interface{}{}
		}
	}
	if len(payload) == 0 {
		payload["v"] = messagePayloadVersion
		payload["text"] = body
	} else {
		if _, ok := payload["v"]; !ok {
			payload["v"] = messagePayloadVersion
		}
		if _, ok := payload["text"]; !ok {
			payload["text"] = ""
		}
	}
	payload["voice_text"] = safeText
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", voiceTranscribeHTTPError{
			Status:  http.StatusInternalServerError,
			Code:    "payload_merge_failed",
			Message: "cannot update message payload",
		}
	}
	return string(encoded), nil
}

func (a *API) transcribeVoiceMessageURL(mediaURL string) (string, error) {
	apiKey := resolveSiliconFlowAPIKey()
	if apiKey == "" {
		return "", voiceTranscribeHTTPError{
			Status:  http.StatusServiceUnavailable,
			Code:    "asr_not_configured",
			Message: "asr not configured",
		}
	}
	audioData, fileName, contentType, err := a.loadVoiceMessageAudio(mediaURL)
	if err != nil {
		return "", err
	}
	text, upstreamCode, upstreamErr := transcribeVoiceAudio(audioData, fileName, contentType, apiKey)
	if upstreamErr != nil {
		log.Printf("voice message asr failed: code=%d err=%v", upstreamCode, upstreamErr)
		return "", voiceTranscribeHTTPError{
			Status:  http.StatusBadGateway,
			Code:    "asr_failed",
			Message: upstreamErr.Error(),
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", voiceTranscribeHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "asr_empty",
			Message: "empty transcription",
		}
	}
	return text, nil
}

func (a *API) loadVoiceMessageAudio(mediaURL string) ([]byte, string, string, error) {
	relativePath, ok := uploadURLToRelativePath(mediaURL)
	if !ok {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusNotFound,
			Code:    "media_not_found",
			Message: "media not found",
		}
	}
	uploadRoot := filepath.Clean(a.cfg.UploadDir)
	fullPath := filepath.Clean(filepath.Join(uploadRoot, filepath.FromSlash(relativePath)))
	relativeCheck, err := filepath.Rel(uploadRoot, fullPath)
	relativeCheck = filepath.ToSlash(relativeCheck)
	if err == nil && !(relativeCheck == ".." || strings.HasPrefix(relativeCheck, "../")) {
		audioData, fileName, contentType, localErr := loadVoiceMessageAudioFromFile(fullPath)
		if localErr == nil {
			return audioData, fileName, contentType, nil
		}
		log.Printf("voice message local audio load failed, fallback to http: path=%s err=%v", fullPath, localErr)
	}

	audioData, fileName, contentType, netErr := a.loadVoiceMessageAudioFromHTTP(mediaURL, relativePath)
	if netErr == nil {
		return audioData, fileName, contentType, nil
	}
	log.Printf("voice message http audio load failed: url=%s err=%v", strings.TrimSpace(mediaURL), netErr)
	return nil, "", "", voiceTranscribeHTTPError{
		Status:  http.StatusNotFound,
		Code:    "media_not_found",
		Message: "media not found",
	}
}

func loadVoiceMessageAudioFromFile(fullPath string) ([]byte, string, string, error) {
	stat, err := os.Stat(fullPath)
	if err != nil || stat.IsDir() {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusNotFound,
			Code:    "media_not_found",
			Message: "media not found",
		}
	}
	if stat.Size() <= 0 {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "empty_audio",
			Message: "empty audio",
		}
	}
	if stat.Size() > maxVoiceASRUploadBytes {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusRequestEntityTooLarge,
			Code:    "audio_too_large",
			Message: "audio too large",
		}
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusInternalServerError,
			Code:    "media_read_failed",
			Message: "cannot read media",
		}
	}
	defer file.Close()
	audioData, err := io.ReadAll(io.LimitReader(file, maxVoiceASRUploadBytes+1))
	if err != nil {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusInternalServerError,
			Code:    "media_read_failed",
			Message: "cannot read media",
		}
	}
	if len(audioData) == 0 {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "empty_audio",
			Message: "empty audio",
		}
	}
	if int64(len(audioData)) > maxVoiceASRUploadBytes {
		return nil, "", "", voiceTranscribeHTTPError{
			Status:  http.StatusRequestEntityTooLarge,
			Code:    "audio_too_large",
			Message: "audio too large",
		}
	}

	fileName := filepath.Base(fullPath)
	if fileName == "" {
		fileName = "voice.3gp"
	}
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	if contentType == "" {
		contentType = "audio/3gpp"
	}
	return audioData, fileName, contentType, nil
}
