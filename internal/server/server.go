package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"hubfly-builder/internal/executor"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
)

type Server struct {
	storage    *storage.Storage
	logManager *logs.LogManager
	manager    *executor.Manager
}

func NewServer(storage *storage.Storage, logManager *logs.LogManager, manager *executor.Manager) *Server {
	return &Server{
		storage:    storage,
		logManager: logManager,
		manager:    manager,
	}
}

func (s *Server) Start(addr string) error {
	r := mux.NewRouter()
	r.HandleFunc("/api/v1/jobs", s.CreateJobHandler).Methods("POST")
	r.HandleFunc("/api/v1/jobs/{id}", s.GetJobHandler).Methods("GET")
	r.HandleFunc("/api/v1/jobs/{id}/logs", s.GetJobLogsHandler).Methods("GET")
	r.HandleFunc("/dev/running-builds", s.GetRunningBuildsHandler).Methods("GET")
	r.HandleFunc("/healthz", HealthCheckHandler).Methods("GET")

	return http.ListenAndServe(addr, r)
}

func (s *Server) CreateJobHandler(w http.ResponseWriter, r *http.Request) {
	var job storage.BuildJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if job.LogPath == "" {
		http.Error(w, "logs not available yet", http.StatusNotFound)
		return
	}

	logs, err := s.logManager.GetLog(job.LogPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write(logs)
}

type RunningBuild struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	UserID    string    `json:"userId"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
	LogPath   string    `json:"logPath"`
}

func (s *Server) GetRunningBuildsHandler(w http.ResponseWriter, r *http.Request) {
	// For now, returns an empty list as per M1.
	// Later, this will query the executor manager.
	runningBuilds := []RunningBuild{}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(runningBuilds)
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
