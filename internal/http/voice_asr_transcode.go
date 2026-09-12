package httpapi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func transcodeForVoiceASR(audioData []byte, fileName, contentType string) ([]byte, string, string, error) {
	if len(audioData) == 0 {
		return nil, "", "", asrError("ffmpeg_transcode_failed: empty audio")
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, "", "", asrError("ffmpeg_not_found")
	}

	tempDir, err := os.MkdirTemp("", "oldchat-asr-*")
	if err != nil {
		return nil, "", "", asrError("ffmpeg_transcode_failed: cannot create temp dir")
	}
	defer os.RemoveAll(tempDir)

	inputExt := detectASRInputExt(fileName, contentType)
	inputPath := filepath.Join(tempDir, "input"+inputExt)
	outputPath := filepath.Join(tempDir, "output.wav")

	if err = os.WriteFile(inputPath, audioData, 0o600); err != nil {
		return nil, "", "", asrError("ffmpeg_transcode_failed: cannot write temp input")
	}

	cmd := exec.Command(ffmpegPath, "-hide_banner", "-loglevel", "error", "-y",
		"-i", inputPath,
		"-ac", "1",
		"-ar", "16000",
		"-f", "wav",
		outputPath,
	)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = runErr.Error()
		}
		if len(msg) > 160 {
			msg = msg[:160] + "..."
		}
		return nil, "", "", asrError("ffmpeg_transcode_failed: " + msg)
	}

	wavData, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, "", "", asrError("ffmpeg_transcode_failed: cannot read wav")
	}
	if len(wavData) == 0 {
		return nil, "", "", asrError("ffmpeg_transcode_failed: empty wav")
	}

	return wavData, withFileExt(fileName, ".wav"), "audio/wav", nil
}

func detectASRInputExt(fileName, contentType string) string {
	name := strings.TrimSpace(fileName)
	ext := strings.ToLower(filepath.Ext(name))
	if ext != "" {
		return ext
	}

	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(lowerType, "3gpp"):
		return ".3gp"
	case strings.Contains(lowerType, "amr"):
		return ".amr"
	case strings.Contains(lowerType, "mp4"):
		return ".m4a"
	case strings.Contains(lowerType, "aac"):
		return ".aac"
	case strings.Contains(lowerType, "wav"), strings.Contains(lowerType, "wave"):
		return ".wav"
	case strings.Contains(lowerType, "mpeg"), strings.Contains(lowerType, "mp3"):
		return ".mp3"
	default:
		return ".bin"
	}
}
