package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	siliconFlowASRURL         = "https://api.siliconflow.cn/v1/audio/transcriptions"
	siliconFlowASRModel       = "TeleAI/TeleSpeechASR"
	maxVoiceASRUploadBytes    = 8 << 20
	maxVoiceASRRequestBytes   = maxVoiceASRUploadBytes + (1 << 20)
	maxVoiceASRResponseBytes  = 2 << 20
)

type voiceASRResponse struct {
	Text string `json:"text"`
}

type siliconFlowASRSuccess struct {
	Text string `json:"text"`
}

type siliconFlowASRError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

func (a *API) handleVoiceASR(w http.ResponseWriter, r *http.Request) {
	_, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	apiKey := resolveSiliconFlowAPIKey()
	if apiKey == "" {
		writeError(w, http.StatusServiceUnavailable, "asr_not_configured", "asr not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxVoiceASRRequestBytes)
	limitUploadBody(r)
	if err := r.ParseMultipartForm(maxVoiceASRRequestBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "missing file")
		return
	}
	defer file.Close()

	audioData, err := io.ReadAll(io.LimitReader(file, maxVoiceASRUploadBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_upload", "invalid upload")
		return
	}
	if len(audioData) == 0 {
		writeError(w, http.StatusBadRequest, "empty_audio", "empty audio")
		return
	}
	if int64(len(audioData)) > maxVoiceASRUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "audio_too_large", "audio too large")
		return
	}

	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	if contentType == "" {
		contentType = "audio/3gpp"
	}

	text, upstreamCode, upstreamErr := transcribeVoiceAudio(audioData, header.Filename, contentType, apiKey)
	if upstreamErr != nil {
		log.Printf("voice asr failed: code=%d err=%v", upstreamCode, upstreamErr)
		writeError(w, http.StatusBadGateway, "asr_failed", upstreamErr.Error())
		return
	}
	if strings.TrimSpace(text) == "" {
		writeError(w, http.StatusUnprocessableEntity, "asr_empty", "empty transcription")
		return
	}
	writeJSON(w, http.StatusOK, voiceASRResponse{Text: strings.TrimSpace(text)})
}

func transcribeVoiceAudio(audioData []byte, fileName, contentType, apiKey string) (string, int, error) {
	text, upstreamCode, upstreamErr := requestSiliconFlowASR(audioData, fileName, contentType, apiKey)
	if upstreamErr != nil && shouldRetryAsMP4(contentType, fileName) {
		text, upstreamCode, upstreamErr = requestSiliconFlowASR(audioData, withFileExt(fileName, ".m4a"), "audio/mp4", apiKey)
	}
	var transcodeErr error
	if upstreamErr != nil && shouldRetryWithTranscode(contentType, fileName) {
		var transcodedData []byte
		var transcodedName string
		var transcodedType string
		transcodedData, transcodedName, transcodedType, transcodeErr = transcodeForVoiceASR(audioData, fileName, contentType)
		if transcodeErr == nil {
			text, upstreamCode, upstreamErr = requestSiliconFlowASR(transcodedData, transcodedName, transcodedType, apiKey)
		}
	}
	if upstreamErr != nil && transcodeErr != nil {
		log.Printf("voice asr transcode fallback failed: %v", transcodeErr)
		if isFormatLikelyUnsupported(upstreamErr.Error()) {
			upstreamErr = asrError(upstreamErr.Error() + "; fallback=" + transcodeErr.Error())
		}
	}
	return text, upstreamCode, upstreamErr
}

func resolveSiliconFlowAPIKey() string {
	if key := strings.TrimSpace(os.Getenv("SILICONFLOW_API_KEY")); key != "" {
		return key
	}
	return ""
}

func requestSiliconFlowASR(audioData []byte, fileName, contentType, apiKey string) (string, int, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	partHeader := textproto.MIMEHeader{}
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename="`+sanitizeASRFileName(fileName)+`"`)
	safeType := strings.TrimSpace(contentType)
	if safeType == "" {
		safeType = "application/octet-stream"
	}
	partHeader.Set("Content-Type", safeType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return "", 0, err
	}
	if _, err = part.Write(audioData); err != nil {
		return "", 0, err
	}
	if err = writer.WriteField("model", siliconFlowASRModel); err != nil {
		return "", 0, err
	}
	if err = writer.Close(); err != nil {
		return "", 0, err
	}

	req, err := http.NewRequest(http.MethodPost, siliconFlowASRURL, &body)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(io.LimitReader(resp.Body, maxVoiceASRResponseBytes))
	if err != nil {
		return "", resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, parseSiliconFlowASRError(respData, resp.Status)
	}

	var parsed siliconFlowASRSuccess
	if err := json.Unmarshal(respData, &parsed); err != nil {
		return "", resp.StatusCode, err
	}
	return parsed.Text, resp.StatusCode, nil
}

func parseSiliconFlowASRError(data []byte, fallback string) error {
	var parsed siliconFlowASRError
	if err := json.Unmarshal(data, &parsed); err == nil {
		if msg := strings.TrimSpace(parsed.Error.Message); msg != "" {
			return asrError(msg)
		}
		if msg := strings.TrimSpace(parsed.Message); msg != "" {
			return asrError(msg)
		}
	}
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		msg = strings.TrimSpace(fallback)
	}
	if msg == "" {
		msg = "upstream asr error"
	}
	return asrError(msg)
}

func sanitizeASRFileName(fileName string) string {
	name := strings.TrimSpace(fileName)
	if name == "" {
		return "voice.3gp"
	}
	name = strings.ReplaceAll(name, "\r", "_")
	name = strings.ReplaceAll(name, "\n", "_")
	name = strings.ReplaceAll(name, "\"", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if len(name) > 120 {
		name = name[len(name)-120:]
	}
	if strings.TrimSpace(name) == "" {
		return "voice.3gp"
	}
	return name
}

func shouldRetryAsMP4(contentType, fileName string) bool {
	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	lowerName := strings.ToLower(strings.TrimSpace(fileName))
	return strings.Contains(lowerType, "3gpp") ||
		strings.HasSuffix(lowerName, ".3gp") ||
		strings.HasSuffix(lowerName, ".3gpp")
}

func shouldRetryWithTranscode(contentType, fileName string) bool {
	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	lowerName := strings.ToLower(strings.TrimSpace(fileName))
	return strings.Contains(lowerType, "3gpp") ||
		strings.Contains(lowerType, "amr") ||
		strings.HasSuffix(lowerName, ".3gp") ||
		strings.HasSuffix(lowerName, ".3gpp") ||
		strings.HasSuffix(lowerName, ".amr")
}

func isFormatLikelyUnsupported(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "unsupported") ||
		strings.Contains(lower, "format") ||
		strings.Contains(lower, "codec") ||
		strings.Contains(lower, "decode") ||
		strings.Contains(lower, "invalid file")
}

func withFileExt(fileName, ext string) string {
	name := strings.TrimSpace(fileName)
	if name == "" {
		return "voice" + ext
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" {
		base = "voice"
	}
	return base + ext
}

type asrError string

func (e asrError) Error() string {
	return string(e)
}

