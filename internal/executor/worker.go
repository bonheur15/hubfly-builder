package executor

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/driver"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
)

type Worker struct {
	job        *storage.BuildJob
	storage    *storage.Storage
	logManager *logs.LogManager
	allowlist  *allowlist.AllowedCommands
	buildkit   *driver.BuildKit
	registry   string
	logFile    *os.File
	logWriter  io.Writer
	workDir    string
}

func NewWorker(job *storage.BuildJob, storage *storage.Storage, logManager *logs.LogManager, allowlist *allowlist.AllowedCommands, buildkit *driver.BuildKit, registry string) *Worker {
	return &Worker{
		job:        job,
		storage:    storage,
		logManager: logManager,
		allowlist:  allowlist,
		buildkit:   buildkit,
		registry:   registry,
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
	w.logWriter = io.MultiWriter(os.Stdout, w.logFile)

	if err := w.storage.UpdateJobLogPath(w.job.ID, logPath); err != nil {
		w.log("ERROR: could not update log path: %v", err)
		w.failJob("internal server error")
		return
	}

	if err := w.storage.UpdateJobStatus(w.job.ID, "building"); err != nil {
		w.log("ERROR: could not update status to 'building': %v", err)
		w.failJob("internal server error")
		return
	}

	// Create workspace
	w.workDir, err = os.MkdirTemp("", fmt.Sprintf("hubfly-builder-ws-%s-", w.job.ID))
	if err != nil {
		w.log("ERROR: could not create workspace: %v", err)
		w.failJob("internal server error")
		return
	}
	defer os.RemoveAll(w.workDir)
	w.log("Created workspace: %s", w.workDir)

	// --- Clone repository ---
	w.log("Cloning repository from %s...", w.job.SourceInfo.GitRepository)
	cloneCmd := exec.Command("git", "clone", w.job.SourceInfo.GitRepository, w.workDir)
	if err := w.executeCommand(cloneCmd); err != nil {
		w.log("ERROR: failed to clone repository: %v", err)
		w.failJob("failed to clone repository")
		return
	}
	w.log("Repository cloned successfully.")

	// --- Pre-build command ---
	if w.job.BuildConfig.PrebuildCommand != "" {
		w.log("Pre-build command specified: %s", w.job.BuildConfig.PrebuildCommand)
		if !allowlist.IsCommandAllowed(w.job.BuildConfig.PrebuildCommand, w.allowlist.Prebuild) {
			w.log("ERROR: pre-build command is not allowed: %s", w.job.BuildConfig.PrebuildCommand)
			w.failJob("pre-build command not allowed")
			return
		}

		preBuildCmd := exec.Command("sh", "-c", w.job.BuildConfig.PrebuildCommand)
		preBuildCmd.Dir = w.workDir
		if err := w.executeCommand(preBuildCmd); err != nil {
			w.log("ERROR: pre-build command failed: %v", err)
			w.failJob("pre-build command failed")
			return
		}
		w.log("Pre-build command finished successfully.")
	}

	// --- Build with BuildKit ---
	dockerfilePath := filepath.Join(w.workDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err == nil {
		w.log("Dockerfile found, starting BuildKit build...")
		imageTag := w.generateImageTag()
		w.log("Image tag: %s", imageTag)

		opts := driver.BuildOpts{
			ContextPath:    w.workDir,
			Dockerfileath: w.workDir, // Dockerfile is at the root of the context
			ImageTag:       imageTag,
		}
		buildCmd := w.buildkit.BuildCommand(opts)
		if err := w.executeCommand(buildCmd); err != nil {
			w.log("ERROR: BuildKit build failed: %v", err)
			w.failJob("BuildKit build failed")
			return
		}
		w.log("BuildKit build and push successful.")
		// TODO: Update job with image tag
	} else {
		w.log("No Dockerfile found, skipping BuildKit build.")
		// TODO: Implement other build strategies (e.g., buildpacks)
	}

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
	fmt.Fprintf(w.logWriter, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339), logLine)
}

func (w *Worker) executeCommand(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	go w.streamPipe(stdout)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	go w.streamPipe(stderr)

	w.log("Executing: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		return err
	}

	return cmd.Wait()
}

func (w *Worker) streamPipe(pipe io.Reader) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		w.log(scanner.Text())
	}
}

func (w *Worker) generateImageTag() string {
	// registry/<userid>/<projectid>:<commitSha>-b<buildId>-v<timestamp>
	ts := time.Now().UTC().Format("20060102T150405Z")
	shortSha := w.job.SourceInfo.CommitSha
	if len(shortSha) > 12 {
		shortSha = shortSha[:12]
	}
	sanitizedUserID := sanitize(w.job.UserID)
	sanitizedProjectID := sanitize(w.job.ProjectID)
	return fmt.Sprintf("%s/%s/%s:%s-b%s-v%s", w.registry, sanitizedUserID, sanitizedProjectID, shortSha, w.job.ID, ts)
}

func sanitize(s string) string {
	// Basic sanitization for registry path components
	return strings.ToLower(strings.ReplaceAll(s, "_", "-"))
}