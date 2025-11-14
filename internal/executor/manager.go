package executor

import (
	"log"
	"sync"

	"hubfly-builder/internal/storage"
)

type Manager struct {
	storage       *storage.Storage
	maxConcurrent int
	activeBuilds  map[string]bool
	mu            sync.Mutex
}

func NewManager(storage *storage.Storage, maxConcurrent int) *Manager {
	return &Manager{
		storage:       storage,
		maxConcurrent: maxConcurrent,
		activeBuilds:  make(map[string]bool),
	}
}

func (m *Manager) Start() {
	log.Println("Executor manager started")
	// This is a simplified loop. A real implementation would use a more robust
	// queuing and scheduling mechanism.
	for {
		m.mu.Lock()
		if len(m.activeBuilds) < m.maxConcurrent {
			// TODO: Get a pending job from storage and start a worker
		}
		m.mu.Unlock()
	}
}
