package executor

import (
	"log"

	"hubfly-builder/internal/storage"
)

type Worker struct {
	job *storage.BuildJob
}

func NewWorker(job *storage.BuildJob) *Worker {
	return &Worker{job: job}
}

func (w *Worker) Run() {
	log.Printf("Starting build for job %s", w.job.ID)

	// TODO: Implement the build process:
	// 1. Clone repository
	// 2. Auto-detect build commands
	// 3. Execute pre-build, build, and run commands
	// 4. Push image to registry
	// 5. Report status to backend

	log.Printf("Finished build for job %s", w.job.ID)
}
