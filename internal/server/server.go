package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gorilla/mux"
	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/autodetect"
	"hubfly-builder/internal/executor"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
)

type Server struct {
	storage    *storage.Storage
	logManager *logs.LogManager
	manager    *executor.Manager
	allowlist  *allowlist.AllowedCommands
}

func NewServer(storage *storage.Storage, logManager *logs.LogManager, manager *executor.Manager, allowlist *allowlist.AllowedCommands) *Server {
	return &Server{
		storage:    storage,
		logManager: logManager,
		manager:    manager,
		allowlist:  allowlist,
	}
}

func (s *Server) Start(addr string) error {
	r := mux.NewRouter()
	r.HandleFunc("/api/v1/jobs", s.CreateJobHandler).Methods("POST")
	r.HandleFunc("/api/v1/jobs/{id}", s.GetJobHandler).Methods("GET")
	r.HandleFunc("/api/v1/jobs/{id}/logs", s.GetJobLogsHandler).Methods("GET")
	r.HandleFunc("/dev/running-builds", s.GetRunningBuildsHandler).Methods("GET")
	r.HandleFunc("/dev/reset-db", s.ResetDatabaseHandler).Methods("POST")
	r.HandleFunc("/healthz", HealthCheckHandler).Methods("GET")

	return http.ListenAndServe(addr, r)
}

func (s *Server) CreateJobHandler(w http.ResponseWriter, r *http.Request) {
	var job storage.BuildJob
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("Incoming %s %s payload: %s", r.Method, r.URL.Path, string(body))
	if err := json.Unmarshal(body, &job); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if job.BuildConfig.IsAutoBuild {
		// For auto-build, we need to clone the repo first to inspect it.
		// This is a simplified approach. A more robust solution might involve
		// a separate service to handle repo inspection before creating the job.
		tempDir, err := os.MkdirTemp("", "hubfly-builder-autodetect-")
		if err != nil {
			http.Error(w, "failed to create temp dir for autodetect", http.StatusInternalServerError)
			return
		}
		defer os.RemoveAll(tempDir)

		cloneCmd := exec.Command("git", "clone", job.SourceInfo.GitRepository, tempDir)
		if err := cloneCmd.Run(); err != nil {
			http.Error(w, "failed to clone repository for autodetect", http.StatusBadRequest)
			return
		}

		if job.SourceInfo.Ref != "" {
			if err := exec.Command("git", "-C", tempDir, "checkout", job.SourceInfo.Ref).Run(); err != nil {
				http.Error(w, fmt.Sprintf("failed to checkout ref %s", job.SourceInfo.Ref), http.StatusBadRequest)
				return
			}
		}
		if job.SourceInfo.CommitSha != "" {
			if err := exec.Command("git", "-C", tempDir, "checkout", job.SourceInfo.CommitSha).Run(); err != nil {
				http.Error(w, fmt.Sprintf("failed to checkout commit %s", job.SourceInfo.CommitSha), http.StatusBadRequest)
				return
			}
		}

		inspectDir := tempDir
		if job.SourceInfo.WorkingDir != "" {
			inspectDir = filepath.Join(tempDir, job.SourceInfo.WorkingDir)
		}

		detectedConfig, err := autodetect.AutoDetectBuildConfig(inspectDir, s.allowlist)
		if err != nil {
			http.Error(w, "failed to autodetect build config", http.StatusInternalServerError)
			return
		}
		job.BuildConfig = storage.BuildConfig{
			IsAutoBuild:       detectedConfig.IsAutoBuild,
			Runtime:           detectedConfig.Runtime,
			Version:           detectedConfig.Version,
			PrebuildCommand:   detectedConfig.PrebuildCommand,
			BuildCommand:      detectedConfig.BuildCommand,
			RunCommand:        detectedConfig.RunCommand,
			TimeoutSeconds:    job.BuildConfig.TimeoutSeconds,   // Keep original timeout
			ResourceLimits:    job.BuildConfig.ResourceLimits,   // Keep original resource limits
			DockerfileContent: detectedConfig.DockerfileContent,
		}
	}

	if err := s.storage.CreateJob(&job); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Signal the manager that a new job is available
	s.manager.SignalNewJob()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(job)
}

func (s *Server) GetJobHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	job, err := s.storage.GetJob(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJobNotFound(w)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(job)
}

func (s *Server) GetJobLogsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	job, err := s.storage.GetJob(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJobNotFound(w)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if job.LogPath == "" {
		writeBuildLogNotFound(w)
		return
	}

	logs, err := s.logManager.GetLog(job.LogPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeBuildLogNotFound(w)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write(logs)
}

func writeBuildLogNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   "BUILD_LOG_NOT_FOUND",
		"message": "build log not found",
	})
}

func writeJobNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   "JOB_NOT_FOUND",
		"message": "job not found",
	})
}

func (s *Server) GetRunningBuildsHandler(w http.ResponseWriter, r *http.Request) {
	activeIDs := s.manager.GetActiveBuilds()
	runningBuilds := []storage.BuildJob{}

	for _, id := range activeIDs {
		job, err := s.storage.GetJob(id)
		if err != nil {
			// Log the error but don't fail the whole request
			log.Printf("WARN: could not get job details for active job %s: %v", id, err)
			continue
		}
		runningBuilds = append(runningBuilds, *job)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(runningBuilds)
}

func (s *Server) ResetDatabaseHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.storage.ResetDatabase(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Database reset successful")
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
