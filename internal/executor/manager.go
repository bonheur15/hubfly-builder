package executor

import (
	"log"
	"sync"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
)

type Manager struct {
	storage       *storage.Storage
	logManager    *logs.LogManager
	allowlist     *allowlist.AllowedCommands
	maxConcurrent int
	activeBuilds  map[string]bool
	mu            sync.Mutex
	newJobSignal  chan struct{}
}

func NewManager(storage *storage.Storage, logManager *logs.LogManager, allowlist *allowlist.AllowedCommands, maxConcurrent int) *Manager {
	return &Manager{
		storage:       storage,
		logManager:    logManager,
		allowlist:     allowlist,
		maxConcurrent: maxConcurrent,
		activeBuilds:  make(map[string]bool),
		newJobSignal:  make(chan struct{}, 1),
	}
}

// SignalNewJob tells the manager to check for a new pending job.
func (m *Manager) SignalNewJob() {
	select {
	case m.newJobSignal <- struct{}{}:
	default:
	}
}

func (m *Manager) Start() {
	log.Println("Executor manager started")
	ticker := time.NewTicker(5 * time.Second) // Poll for jobs periodically
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.tryToDispatchJob()
		case <-m.newJobSignal:
			m.tryToDispatchJob()
		}
	}
}

func (m *Manager) tryToDispatchJob() {
	m.mu.Lock()
	if len(m.activeBuilds) >= m.maxConcurrent {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	job, err := m.storage.GetPendingJob()
	if err != nil {
		// This is expected if there are no pending jobs (sql.ErrNoRows)
		return
	}

	m.mu.Lock()
	m.activeBuilds[job.ID] = true
	m.mu.Unlock()

	log.Printf("Dispatching job %s", job.ID)
	if err := m.storage.UpdateJobStatus(job.ID, "claimed"); err != nil {
		log.Printf("ERROR: could not update job status for %s: %v", job.ID, err)
		return
	}

	worker := NewWorker(job, m.storage, m.logManager, m.allowlist)
	go func() {
		worker.Run()
		m.mu.Lock()
		delete(m.activeBuilds, job.ID)
		m.mu.Unlock()
	}()
}
