package main

import (
	"log"
	"os"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/driver"
	"hubfly-builder/internal/executor"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/server"
	"hubfly-builder/internal/storage"
)

const maxConcurrentBuilds = 3

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

	allowedCommands, err := allowlist.LoadAllowedCommands("configs/allowed-commands.json")
	if err != nil {
		log.Fatalf("could not load allowed commands: %s\n", err)
	}

	storage, err := storage.NewStorage("./hubfly-builder.sqlite")
	if err != nil {
		log.Fatalf("could not create storage: %s\n", err)
	}

	logManager, err := logs.NewLogManager("./log")
	if err != nil {
		log.Fatalf("could not create log manager: %s\n", err)
	}

	buildkit := driver.NewBuildKit(buildkitAddr)

	manager := executor.NewManager(storage, logManager, allowedCommands, buildkit, registry, maxConcurrentBuilds)
	go manager.Start()

	server := server.NewServer(storage, logManager, manager)

	log.Println("Server listening on :8080")
	if err := server.Start(":8080"); err != nil {
		log.Fatalf("could not start server: %s\n", err)
	}
}
