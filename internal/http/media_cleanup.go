package httpapi

import (
    "os"
    "path/filepath"
    "strings"
    "time"
)

const (
    mediaRetention       = 72 * time.Hour
    mediaCleanupInterval = 6 * time.Hour
)

func startMediaCleanup(uploadDir string) {
    if uploadDir == "" {
        return
    }
    go func() {
        cleanupMedia(uploadDir)
        ticker := time.NewTicker(mediaCleanupInterval)
        defer ticker.Stop()
        for range ticker.C {
            cleanupMedia(uploadDir)
        }
    }()
}

func cleanupMedia(uploadDir string) {
    dir := filepath.Join(uploadDir, "media")
    entries, err := os.ReadDir(dir)
    if err != nil {
        return
    }
    cutoff := time.Now().Add(-mediaRetention)
    for _, entry := range entries {
        if entry.IsDir() {
            continue
        }
        name := strings.ToLower(entry.Name())
        if !(strings.HasSuffix(name, ".mp4") || strings.HasSuffix(name, ".3gp") || strings.HasPrefix(name, "vthumb-")) {
            continue
        }
        info, err := entry.Info()
        if err != nil {
            continue
        }
        if info.ModTime().Before(cutoff) {
            _ = os.Remove(filepath.Join(dir, entry.Name()))
        }
    }
}
