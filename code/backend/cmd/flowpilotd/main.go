package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ghoul02015/flowpilot/internal/docker"
	"github.com/ghoul02015/flowpilot/internal/hardware"
	"github.com/ghoul02015/flowpilot/internal/server"
)

func main() {
	address := environment("FLOWPILOT_ADDR", ":18080")
	staticDir := environment("FLOWPILOT_STATIC_DIR", "./web")
	socketPath := environment("FLOWPILOT_DOCKER_SOCKET", "/var/run/docker.sock")
	hardwareCollector := hardware.NewCollector(hardware.Config{
		ProcRoot:     os.Getenv("FLOWPILOT_PROC_ROOT"),
		HostRoot:     os.Getenv("FLOWPILOT_HOST_ROOT"),
		DataDir:      os.Getenv("FLOWPILOT_DATA_DIR"),
		NvidiaSMIBin: os.Getenv("FLOWPILOT_NVIDIA_SMI"),
	})

	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.New(staticDir, docker.NewClient(socketPath), hardwareCollector),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-shutdownContext.Done()
		context, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(context); err != nil {
			slog.Error("HTTP server shutdown failed", "error", err)
		}
	}()

	slog.Info("FlowPilot backend started", "version", server.Version, "address", address, "staticDir", staticDir)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server failed", "error", err)
		os.Exit(1)
	}
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
