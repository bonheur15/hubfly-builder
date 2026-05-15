package executor

import (
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/api"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
	"os"
)

const maxRetries = 0

type Manager struct {
	storage       *storage.Storage
	logManager    *logs.LogManager
	allowlist     *allowlist.AllowedCommands
	apiClient     *api.Client
	maxConcurrent int
	lockfilePath  string
	activeBuilds  map[string]bool
	activeUsers   map[string]bool
	mu            sync.Mutex
	newJobSignal  chan struct{}
}

func NewManager(storage *storage.Storage, logManager *logs.LogManager, allowlist *allowlist.AllowedCommands, apiClient *api.Client, maxConcurrent int, lockfilePath string) *Manager {
	m := &Manager{
		storage:       storage,
		logManager:    logManager,
		allowlist:     allowlist,
		apiClient:     apiClient,
		maxConcurrent: maxConcurrent,
		lockfilePath:  lockfilePath,
		activeBuilds:  make(map[string]bool),
		activeUsers:   make(map[string]bool),
		newJobSignal:  make(chan struct{}, 1),
	}
	m.updateLockfile()
	return m
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
	excludeUserIDs := make([]string, 0, len(m.activeUsers))
	for id := range m.activeUsers {
		excludeUserIDs = append(excludeUserIDs, id)
	}
	m.mu.Unlock()

	job, err := m.storage.GetPendingJobExcludingUsers(excludeUserIDs)
	if err != nil {
		return
	}
	if strings.TrimSpace(job.UserID) == "" {
		log.Printf("ERROR: job %s missing userId; dropping", job.ID)
		_ = m.storage.UpdateJobStatus(job.ID, "failed")
		return
	}

	m.mu.Lock()
	m.activeBuilds[job.ID] = true
	m.activeUsers[job.UserID] = true
	m.updateLockfileLocked()
	m.mu.Unlock()

	if err := m.storage.UpdateJobStatus(job.ID, "claimed"); err != nil {
		log.Printf("ERROR: could not update job status for %s: %v", job.ID, err)
		m.mu.Lock()
		delete(m.activeBuilds, job.ID)
		delete(m.activeUsers, job.UserID)
		m.updateLockfileLocked()
		m.mu.Unlock()
		return
	}

	worker := NewWorker(job, m.storage, m.logManager, m.allowlist, m.apiClient)
	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.activeBuilds, job.ID)
			delete(m.activeUsers, job.UserID)
			m.updateLockfileLocked()
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

func (m *Manager) updateLockfileLocked() {
	if m.lockfilePath == "" {
		return
	}
	if len(m.activeBuilds) > 0 {
		_ = os.WriteFile(m.lockfilePath, []byte("busy"), 0644)
	} else {
		_ = os.Remove(m.lockfilePath)
	}
}

func (m *Manager) updateLockfile() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateLockfileLocked()
}
