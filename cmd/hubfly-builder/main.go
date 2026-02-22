package main

import (
	"encoding/json"
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

type EnvConfig struct {
	RegistryURL string `json:"REGISTRY_URL"`
	CallbackURL string `json:"CALLBACK_URL"`
}

func loadOrInitEnvConfig() {
	filename := "configs/env.json"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// Ensure configs directory exists
		if err := os.MkdirAll("configs", 0755); err != nil {
			log.Printf("WARN: could not create configs directory: %v", err)
			return
		}

		config := EnvConfig{
			RegistryURL: "",
			CallbackURL: "",
		}
		data, _ := json.MarshalIndent(config, "", "  ")
		if err := os.WriteFile(filename, data, 0644); err != nil {
			log.Printf("WARN: could not create default %s: %v", filename, err)
		} else {
			log.Printf("Created default %s", filename)
		}
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("WARN: could not read %s: %v", filename, err)
		return
	}

	var config EnvConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("WARN: could not parse %s: %v", filename, err)
		return
	}

	// Set environment variables if they are present in the config
	if config.RegistryURL != "" {
		os.Setenv("REGISTRY_URL", config.RegistryURL)
	}
	if config.CallbackURL != "" {
		os.Setenv("CALLBACK_URL", config.CallbackURL)
	}
}

func main() {
	loadOrInitEnvConfig()

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

	systemLogPath, systemLogFile, err := logManager.CreateSystemLogFile()
	if err != nil {
		log.Fatalf("could not create system log file: %s\n", err)
	}
	defer systemLogFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, systemLogFile))
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("System log file: %s", systemLogPath)
	log.Printf("Env: REGISTRY_URL=%q CALLBACK_URL=%q", os.Getenv("REGISTRY_URL"), os.Getenv("CALLBACK_URL"))
	log.Printf("Effective: REGISTRY_URL=%q CALLBACK_URL=%q", registry, callbackURL)
	if err := driver.CleanupOrphanedEphemeralBuildKits(); err != nil {
		log.Printf("WARN: could not cleanup stale ephemeral BuildKit containers: %v", err)
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

	apiClient := api.NewClient(callbackURL)

	manager := executor.NewManager(storage, logManager, allowedCommands, apiClient, registry, maxConcurrentBuilds)
	go manager.Start()

	server := server.NewServer(storage, logManager, manager, allowedCommands)

	log.Println("Server listening on :8781")
	if err := server.Start(":8781"); err != nil {
		log.Fatalf("could not start server: %s\n", err)
	}
}
