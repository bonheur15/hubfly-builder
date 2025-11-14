package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"hubfly-builder/internal/storage"
)

type Server struct {
	storage *storage.Storage
}

func NewServer(storage *storage.Storage) *Server {
	return &Server{storage: storage}
}

func (s *Server) Start(addr string) error {
	r := mux.NewRouter()
	r.HandleFunc("/api/v1/jobs", s.CreateJobHandler).Methods("POST")
	r.HandleFunc("/api/v1/jobs/{id}", s.GetJobHandler).Methods("GET")
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

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(job)
}

func (s *Server) GetJobHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	job, err := s.storage.GetJob(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(job)
}

func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
