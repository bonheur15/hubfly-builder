package executor

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
)

type Worker struct {
	job        *storage.BuildJob
	storage    *storage.Storage
	logManager *logs.LogManager
	logFile    *os.File
	logWriter  io.Writer
}

func NewWorker(job *storage.BuildJob, storage *storage.Storage, logManager *logs.LogManager) *Worker {
	return &Worker{
		job:        job,
		storage:    storage,
		logManager: logManager,
	}
}

func (w *Worker) Run() {
	log.Printf("Starting build for job %s", w.job.ID)
	var err error

	// Create log file
	logPath, logFile, err := w.logManager.CreateLogFile(w.job.ID)
	if err != nil {
		log.Printf("ERROR: could not create log file for job %s: %v", w.job.ID, err)
		w.failJob("failed to create log file")
		return
	}
	w.logFile = logFile
	defer w.logFile.Close()

	// Use a multi-writer to log to both stdout and the file
	w.logWriter = io.MultiWriter(os.Stdout, w.logFile)

	w.log("Updating log path...")
	if err := w.storage.UpdateJobLogPath(w.job.ID, logPath); err != nil {
		w.log("ERROR: could not update log path: %v", err)
		w.failJob("internal server error")
		return
	}

	w.log("Updating status to 'building'...")
	if err := w.storage.UpdateJobStatus(w.job.ID, "building"); err != nil {
		w.log("ERROR: could not update status to 'building': %v", err)
		w.failJob("internal server error")
		return
	}

	// Simulate build process
	w.log("Simulating build process...")
	time.Sleep(5 * time.Second)
	w.log("Cloning repository from %s", w.job.SourceInfo.GitRepository)
	time.Sleep(5 * time.Second)
	w.log("Auto-detecting build commands...")
	time.Sleep(2 * time.Second)
	w.log("Running pre-build commands...")
	time.Sleep(5 * time.Second)
	w.log("Running build commands...")
	time.Sleep(10 * time.Second)
	w.log("Build successful!")

	w.log("Updating status to 'success'...")
	if err := w.storage.UpdateJobStatus(w.job.ID, "success"); err != nil {
		w.log("ERROR: could not update status to 'success': %v", err)
		w.failJob("internal server error")
		return
	}

	log.Printf("Finished build for job %s", w.job.ID)
}

func (w *Worker) failJob(reason string) {
	log.Printf("Failing job %s: %s", w.job.ID, reason)
	if err := w.storage.UpdateJobStatus(w.job.ID, "failed"); err != nil {
		log.Printf("ERROR: could not update job status to 'failed' for job %s: %v", w.job.ID, err)
	}
}

func (w *Worker) log(format string, args ...interface{}) {
	logLine := fmt.Sprintf(format, args...)
	// In a real implementation, this would be a structured log (JSONL)
	fmt.Fprintf(w.logWriter, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339), logLine)
}