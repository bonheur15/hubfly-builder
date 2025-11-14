package main

import (
	"log"
	"os"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/api"
	"hubfly-builder/internal/driver"
	"hubfly-builder/internal/executor"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/server"
	"hubfly-builder/internal/storage"
)

const maxConcurrentBuilds = 3
const logRetentionDays = 7

func main() {
	// In a real app, get these from config/flags
	buildkitAddr := os.Getenv("BUILDKIT_ADDR")
	if buildkitAddr == "" {
		buildkitAddr = "unix:///run/buildkit/buildkitd.sock"
	}
	registry := os.Getenv("REGISTRY_URL")
	if registry == "" {
		registry = "localhost:5000" // Example registry
	}
	callbackURL := os.Getenv("CALLBACK_URL") // e.g., "http://localhost:3000/api/builds/callback"

	allowedCommands, err := allowlist.LoadAllowedCommands("configs/allowed-commands.json")
	if err != nil {
		log.Fatalf("could not load allowed commands: %s\n", err)
	}

	storage, err := storage.NewStorage("./hubfly-builder.sqlite")
	if err != nil {
		log.Fatalf("could not create storage: %s\n", err)
	}

	if err := storage.ResetInProgressJobs(); err != nil {
		log.Fatalf("could not reset in-progress jobs: %s\n", err)
	}

	logManager, err := logs.NewLogManager("./log")
	if err != nil {
		log.Fatalf("could not create log manager: %s\n", err)
	}

	// Start log cleanup routine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			<-ticker.C
			if err := logManager.Cleanup(logRetentionDays * 24 * time.Hour); err != nil {
				log.Printf("ERROR: log cleanup failed: %v", err)
			}
		}
	}()

	buildkit := driver.NewBuildKit(buildkitAddr)
	apiClient := api.NewClient(callbackURL)

	manager := executor.NewManager(storage, logManager, allowedCommands, buildkit, apiClient, registry, maxConcurrentBuilds)
	go manager.Start()

	server := server.NewServer(storage, logManager, manager, allowedCommands)

	log.Println("Server listening on :8080")
	if err := server.Start(":8080"); err != nil {
		log.Fatalf("could not start server: %s\n", err)
	}
}
