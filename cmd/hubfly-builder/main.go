package main

import (
	"io"
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
	buildkitHost := os.Getenv("BUILDKIT_HOST")

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

	systemLogPath, systemLogFile, err := logManager.CreateSystemLogFile()
	if err != nil {
		log.Fatalf("could not create system log file: %s\n", err)
	}
	defer systemLogFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, systemLogFile))
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("System log file: %s", systemLogPath)
	log.Printf("Env: BUILDKIT_ADDR=%q BUILDKIT_HOST=%q REGISTRY_URL=%q CALLBACK_URL=%q", os.Getenv("BUILDKIT_ADDR"), buildkitHost, os.Getenv("REGISTRY_URL"), os.Getenv("CALLBACK_URL"))
	log.Printf("Effective: BUILDKIT_ADDR=%q REGISTRY_URL=%q CALLBACK_URL=%q", buildkitAddr, registry, callbackURL)

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

	log.Println("Server listening on :8781")
	if err := server.Start(":8781"); err != nil {
		log.Fatalf("could not start server: %s\n", err)
	}
}
