package executor

import (
	"errors"
	"log"
	"sync"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/api"
	"hubfly-builder/internal/driver"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
)

const maxRetries = 0

type Manager struct {
	storage       *storage.Storage
	logManager    *logs.LogManager
	allowlist     *allowlist.AllowedCommands
	buildkit      *driver.BuildKit
	apiClient     *api.Client
	registry      string
	maxConcurrent int
	activeBuilds  map[string]bool
	mu            sync.Mutex
	newJobSignal  chan struct{}
}

func NewManager(storage *storage.Storage, logManager *logs.LogManager, allowlist *allowlist.AllowedCommands, buildkit *driver.BuildKit, apiClient *api.Client, registry string, maxConcurrent int) *Manager {
	return &Manager{
		storage:       storage,
		logManager:    logManager,
		allowlist:     allowlist,
		buildkit:      buildkit,
		apiClient:     apiClient,
		registry:      registry,
		maxConcurrent: maxConcurrent,
		activeBuilds:  make(map[string]bool),
		newJobSignal:  make(chan struct{}, 1),
	}
}

func (m *Manager) SignalNewJob() {
	select {
	case m.newJobSignal <- struct{}{}:
	default:
	}
}

func (m *Manager) Start() {
	log.Println("Executor manager started")
	ticker := time.NewTicker(5 * time.Second)
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
		return
	}

	m.mu.Lock()
	m.activeBuilds[job.ID] = true
	m.mu.Unlock()

	if err := m.storage.UpdateJobStatus(job.ID, "claimed"); err != nil {
		log.Printf("ERROR: could not update job status for %s: %v", job.ID, err)
		m.mu.Lock()
		delete(m.activeBuilds, job.ID)
		m.mu.Unlock()
		return
	}

	worker := NewWorker(job, m.storage, m.logManager, m.allowlist, m.buildkit, m.apiClient, m.registry)
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.activeBuilds, job.ID)
			m.mu.Unlock()
		}()

		if err := worker.Run(); err != nil {
			log.Printf("Worker for job %s finished with error: %v", job.ID, err)
			if errors.Is(err, ErrBuildFailed) {
				m.handleFailedJob(job)
			}
		}
	}()
}

func (m *Manager) handleFailedJob(job *storage.BuildJob) {

	// Refetch job to get latest retry count

	latestJob, err := m.storage.GetJob(job.ID)

	if err != nil {

		log.Printf("ERROR: could not get job %s for retry logic: %v", job.ID, err)

		return

	}

	if latestJob.RetryCount < maxRetries {

		log.Printf("Retrying job %s (attempt %d)", latestJob.ID, latestJob.RetryCount+1)

		if err := m.storage.IncrementJobRetryCount(latestJob.ID); err != nil {

			log.Printf("ERROR: could not increment retry count for job %s: %v", latestJob.ID, err)

			return

		}

		if err := m.storage.UpdateJobStatus(latestJob.ID, "pending"); err != nil {

			log.Printf("ERROR: could not reset job status to pending for retry: %v", err)

		}

		m.SignalNewJob() // Signal to pick it up again

	} else {

		log.Printf("Job %s has reached max retries (%d)", latestJob.ID, maxRetries)

		// The job status is already set to 'failed' by the worker.

	}

}

func (m *Manager) GetActiveBuilds() []string {

	m.mu.Lock()

	defer m.mu.Unlock()

	var ids []string

	for id := range m.activeBuilds {

		ids = append(ids, id)

	}

	return ids

}
