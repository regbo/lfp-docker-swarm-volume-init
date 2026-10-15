package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/moby/moby/client"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	config, err := loadConfig(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		logger.Error("create Docker client", "error", err)
		os.Exit(1)
	}
	defer dockerClient.Close()

	for _, root := range config.hostPaths.roots {
		logger.Info("configured host path filter", "path", root)
	}
	if len(config.hostPaths.roots) == 0 {
		logger.Warn("HOST_PATH_FILTER is empty; all absolute host paths are allowed")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app := newApp(dockerClient, config, logger)
	if err := app.run(ctx); err != nil {
		logger.Error("volume initializer stopped", "error", err)
		os.Exit(1)
	}
}
