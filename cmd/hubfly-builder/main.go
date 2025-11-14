package main

import (
	"log"

	"hubfly-builder/internal/server"
	"hubfly-builder/internal/storage"
)

func main() {
	storage, err := storage.NewStorage("./hubfly-builder.sqlite")
	if err != nil {
		log.Fatalf("could not create storage: %s\n", err)
	}

	server := server.NewServer(storage)

	log.Println("Server listening on :8080")
	if err := server.Start(":8080"); err != nil {
		log.Fatalf("could not start server: %s\n", err)
	}
}
