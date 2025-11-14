package main

import (
	"log"

	"hubfly-builder/internal/executor"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/server"
	"hubfly-builder/internal/storage"
)

const maxConcurrentBuilds = 3

func main() {
	storage, err := storage.NewStorage("./hubfly-builder.sqlite")
	if err != nil {
		log.Fatalf("could not create storage: %s\n", err)
	}

	logManager, err := logs.NewLogManager("./log")
	if err != nil {
		log.Fatalf("could not create log manager: %s\n", err)
	}

	manager := executor.NewManager(storage, logManager, maxConcurrentBuilds)
	go manager.Start()

	server := server.NewServer(storage, logManager, manager)

	log.Println("Server listening on :8080")
	if err := server.Start(":8080"); err != nil {
		log.Fatalf("could not start server: %s\n", err)
	}
}
