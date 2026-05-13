package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/api"
	"hubfly-builder/internal/executor"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/offline"
	"hubfly-builder/internal/server"
	"hubfly-builder/internal/storage"
	"hubfly-builder/internal/uploadserver"
)

const maxConcurrentBuilds = 3
const logRetentionDays = 7

const (
	defaultHubcellBaseURL = "http://127.0.0.1:10012"
	defaultHubcellCLIPath = "/home/destroyer/Desktop/hubfly-cloud/hubcell/hubcell"
	defaultCallbackURL    = "https://hubfly.space/api/builds/callback"
	defaultServerAddr     = ":10008"
	defaultUploadAddr     = ":10011"
)

var version = "dev"

type EnvConfig struct {
	HubcellBaseURL string `json:"HUBCELL_BASE_URL"`
	HubcellCLIPath string `json:"HUBCELL_CLI_PATH"`
	CallbackURL    string `json:"CALLBACK_URL"`
}

func applyDefaultEnvConfig() {
	setEnvIfEmpty("HUBCELL_BASE_URL", defaultHubcellBaseURL)
	setEnvIfEmpty("HUBCELL_CLI_PATH", defaultHubcellCLIPath)
	setEnvIfEmpty("CALLBACK_URL", defaultCallbackURL)
}

func loadOptionalEnvConfig() {
	filename := "configs/env.json"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		log.Printf("Optional config %s not found; using default environment values", filename)
		return
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

	// Only override defaults when the optional config provides a value.
	if config.HubcellBaseURL != "" {
		os.Setenv("HUBCELL_BASE_URL", config.HubcellBaseURL)
	}
	if config.HubcellCLIPath != "" {
		os.Setenv("HUBCELL_CLI_PATH", config.HubcellCLIPath)
	}
	if config.CallbackURL != "" {
		os.Setenv("CALLBACK_URL", config.CallbackURL)
	}
}

func setEnvIfEmpty(key, value string) {
	if os.Getenv(key) == "" {
		os.Setenv(key, value)
	}
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			_, _ = io.WriteString(os.Stdout, version+"\n")
			return
		case "offline":
			if err := offline.Run(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}

	applyDefaultEnvConfig()
	loadOptionalEnvConfig()

	callbackURL := os.Getenv("CALLBACK_URL") // e.g., "http://localhost:3000/api/builds/callback"
	allowedCommands := allowlist.DefaultAllowedCommands()

	if err := os.MkdirAll("data", 0o755); err != nil {
		log.Fatalf("could not create data directory: %s\n", err)
	}

	storage, err := storage.NewStorage("./data/hubfly-builder.sqlite")
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
	log.Printf(
		"Env: HUBCELL_BASE_URL=%q HUBCELL_CLI_PATH=%q CALLBACK_URL=%q",
		os.Getenv("HUBCELL_BASE_URL"),
		os.Getenv("HUBCELL_CLI_PATH"),
		os.Getenv("CALLBACK_URL"),
	)
	log.Printf("Effective: CALLBACK_URL=%q", callbackURL)

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

	manager := executor.NewManager(storage, logManager, allowedCommands, apiClient, maxConcurrentBuilds)
	go manager.Start()

	uploadServer := uploadserver.NewServer(callbackURL)
	go func() {
		log.Printf("Image upload server listening on %s", defaultUploadAddr)
		if err := uploadServer.Start(defaultUploadAddr); err != nil {
			log.Fatalf("could not start image upload server: %s\n", err)
		}
	}()

	server := server.NewServer(storage, logManager, manager, allowedCommands)

	log.Printf("Server listening on %s", defaultServerAddr)
	if err := server.Start(defaultServerAddr); err != nil {
		log.Fatalf("could not start server: %s\n", err)
	}
}
