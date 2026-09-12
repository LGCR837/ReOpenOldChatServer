package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"metrochat/internal/config"
	"metrochat/internal/data"
	httpapi "metrochat/internal/http"
)

func main() {
	log.SetFlags(0)
	httpapi.StartTUI()
	cfg, err := config.Load()
	if err != nil {
		fatalLog("config error: %v", err)
	}
	if cfg.AdminUser == "admin" && cfg.AdminPassword == "admin123456" {
		log.Printf("%s | WARN | using default admin credentials; set ADMIN_USER/ADMIN_PASSWORD", time.Now().Format("15:04:05"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := data.OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		fatalLog("db error: %v", err)
	}
	defer db.Close()

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		fatalLog("upload dir error: %v", err)
	}
	if err := os.MkdirAll(cfg.UpdateDir, 0o755); err != nil {
		fatalLog("update dir error: %v", err)
	}

	h := httpapi.New(cfg, db)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Printf("%s | INFO | listening on :%s", time.Now().Format("15:04:05"), cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatalLog("server error: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	httpapi.StopTUI()
}

// fatalLog stops the TUI, prints the error to stderr, then exits.
// This ensures fatal errors are always visible even when TUI is active
// (TUI redirects log output to an in-memory buffer that os.Exit would discard).
func fatalLog(format string, args ...interface{}) {
	httpapi.StopTUI()
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
