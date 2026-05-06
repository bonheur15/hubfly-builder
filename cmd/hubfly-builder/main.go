package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/api"
	"hubfly-builder/internal/driver"
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
	defaultCallbackURL    = "https://hubfly.space/api/builds/callback"
	defaultRegistryURL    = "127.0.0.1:10009"
	defaultCacheBackend   = "local"
	defaultCacheDir       = "data/buildkit-cache"
	defaultServerAddr     = ":10008"
	defaultUploadAddr     = ":10011"
)

var version = "dev"

const (
	projectCacheRetentionDays = 30
	sharedCacheRetentionDays  = 15
)

type EnvConfig struct {
	HubcellBaseURL string `json:"HUBCELL_BASE_URL"`
	RegistryURL    string `json:"REGISTRY_URL"`
	CallbackURL    string `json:"CALLBACK_URL"`
	CacheBackend   string `json:"BUILDKIT_CACHE_BACKEND"`
	CacheDir       string `json:"BUILDKIT_CACHE_DIR"`
}

func applyDefaultEnvConfig() {
	setEnvIfEmpty("HUBCELL_BASE_URL", defaultHubcellBaseURL)
	setEnvIfEmpty("REGISTRY_URL", defaultRegistryURL)
	setEnvIfEmpty("BUILDKIT_CACHE_BACKEND", defaultCacheBackend)
	setEnvIfEmpty("BUILDKIT_CACHE_DIR", defaultCacheDir)
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
	if config.RegistryURL != "" {
		os.Setenv("REGISTRY_URL", config.RegistryURL)
	}
	if config.CacheBackend != "" {
		os.Setenv("BUILDKIT_CACHE_BACKEND", config.CacheBackend)
	}
	if config.CacheDir != "" {
		os.Setenv("BUILDKIT_CACHE_DIR", config.CacheDir)
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

func ensureBuildKitCacheDir() {
	if strings.ToLower(strings.TrimSpace(os.Getenv("BUILDKIT_CACHE_BACKEND"))) != "local" {
		return
	}

	cacheDir := strings.TrimSpace(os.Getenv("BUILDKIT_CACHE_DIR"))
	if cacheDir == "" {
		return
	}

	absCacheDir, err := filepath.Abs(cacheDir)
	if err != nil {
		log.Printf("WARN: could not resolve BUILDKIT_CACHE_DIR %q: %v", cacheDir, err)
		return
	}
	if err := os.MkdirAll(absCacheDir, 0o755); err != nil {
		log.Printf("WARN: could not create BUILDKIT_CACHE_DIR %q: %v", absCacheDir, err)
		return
	}
	os.Setenv("BUILDKIT_CACHE_DIR", absCacheDir)
}

func cleanupBuildKitCacheDir() {
	if strings.ToLower(strings.TrimSpace(os.Getenv("BUILDKIT_CACHE_BACKEND"))) != "local" {
		return
	}

	cacheDir := strings.TrimSpace(os.Getenv("BUILDKIT_CACHE_DIR"))
	if cacheDir == "" {
		return
	}

	base := filepath.Join(cacheDir, "hubfly-cache")
	pruneSharedCache(filepath.Join(base, "shared"), time.Duration(sharedCacheRetentionDays)*24*time.Hour)
	pruneProjectCache(base, time.Duration(projectCacheRetentionDays)*24*time.Hour)
}

func pruneSharedCache(sharedDir string, retention time.Duration) {
	pruneChildDirs(sharedDir, retention)
}

func pruneProjectCache(baseDir string, retention time.Duration) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("WARN: could not read cache directory %q: %v", baseDir, err)
		}
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "shared" {
			continue
		}
		userDir := filepath.Join(baseDir, entry.Name())
		pruneChildDirs(userDir, retention)
	}
}

func pruneChildDirs(root string, retention time.Duration) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("WARN: could not read cache directory %q: %v", root, err)
		}
		return
	}

	cutoff := time.Now().Add(-retention)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			log.Printf("WARN: could not stat cache directory %q: %v", fullPath, err)
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(fullPath); err != nil {
				log.Printf("WARN: could not remove cache directory %q: %v", fullPath, err)
			}
		}
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
	ensureBuildKitCacheDir()
	cleanupBuildKitCacheDir()

	registry := os.Getenv("REGISTRY_URL")
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
		"Env: HUBCELL_BASE_URL=%q REGISTRY_URL=%q CALLBACK_URL=%q",
		os.Getenv("HUBCELL_BASE_URL"),
		os.Getenv("REGISTRY_URL"),
		os.Getenv("CALLBACK_URL"),
	)
	log.Printf("Effective: REGISTRY_URL=%q CALLBACK_URL=%q", registry, callbackURL)
	if err := driver.CleanupOrphanedEphemeralBuildKits(); err != nil {
		log.Printf("WARN: could not cleanup stale ephemeral BuildKit cells: %v", err)
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

	uploadServer := uploadserver.NewServer(callbackURL, registry)
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
