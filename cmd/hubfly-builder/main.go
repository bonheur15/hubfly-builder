package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
)

func main() {
	fmt.Println("Starting hubfly-builder...")

	// Create a new router
	r := mux.NewRouter()

	// Define routes
	r.HandleFunc("/healthz", HealthCheckHandler).Methods("GET")

	// Start the server
	log.Println("Server listening on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("could not start server: %s\n", err)
	}
}

// HealthCheckHandler returns a 200 OK status
func HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
