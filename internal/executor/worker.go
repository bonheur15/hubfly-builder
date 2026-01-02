package executor

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/api"
	"hubfly-builder/internal/autodetect"
	"hubfly-builder/internal/driver"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
)

var ErrBuildFailed = errors.New("build failed")

type Worker struct {
	job        *storage.BuildJob
	storage    *storage.Storage
	logManager *logs.LogManager
	allowlist  *allowlist.AllowedCommands
	buildkit   *driver.BuildKit
	apiClient  *api.Client
	registry   string
	logFile    *os.File
	logWriter  io.Writer
	workDir    string
}

func NewWorker(job *storage.BuildJob, storage *storage.Storage, logManager *logs.LogManager, allowlist *allowlist.AllowedCommands, buildkit *driver.BuildKit, apiClient *api.Client, registry string) *Worker {
	return &Worker{
		job:        job,
		storage:    storage,
		logManager: logManager,
		allowlist:  allowlist,
		buildkit:   buildkit,
		apiClient:  apiClient,
		registry:   registry,
	}
}

func (w *Worker) Run() error {
	log.Printf("Starting build for job %s", w.job.ID)
	w.job.StartedAt = sql.NullTime{Time: time.Now(), Valid: true}

	logPath, logFile, err := w.logManager.CreateLogFile(w.job.ID)
	if err != nil {
		log.Printf("ERROR: could not create log file for job %s: %v", w.job.ID, err)
		return w.failJob("failed to create log file")
	}
	w.job.LogPath = logPath
	w.logFile = logFile
	defer w.logFile.Close()
	w.logWriter = io.MultiWriter(os.Stdout, w.logFile)

	if err := w.storage.UpdateJobLogPath(w.job.ID, logPath); err != nil {
		w.log("ERROR: could not update log path: %v", err)
		return w.failJob("internal server error")
	}

	if err := w.storage.UpdateJobStatus(w.job.ID, "building"); err != nil {
		w.log("ERROR: could not update status to 'building': %v", err)
		return w.failJob("internal server error")
	}

	w.workDir, err = os.MkdirTemp("", fmt.Sprintf("hubfly-builder-ws-%s-", w.job.ID))
	if err != nil {
		w.log("ERROR: could not create workspace: %v", err)
		return w.failJob("internal server error")
	}
	defer os.RemoveAll(w.workDir)
	w.log("Created workspace: %s", w.workDir)

	cloneCmd := exec.Command("git", "clone", w.job.SourceInfo.GitRepository, w.workDir)
	if err := w.executeCommand(cloneCmd); err != nil {
		w.log("ERROR: failed to clone repository: %v", err)
		return w.failJob("failed to clone repository")
	}
	w.log("Repository cloned successfully.")

	dockerfilePath := filepath.Join(w.workDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err == nil {
		w.log("Dockerfile found, starting BuildKit build...")

		// Warn if PrebuildCommand is ignored
		if w.job.BuildConfig.PrebuildCommand != "" {
			w.log("WARNING: PrebuildCommand '%s' is ignored because a Dockerfile was provided. Please include pre-build steps in your Dockerfile.", w.job.BuildConfig.PrebuildCommand)
		}

		imageTag := w.generateImageTag()
		w.log("Image tag: %s", imageTag)

		opts := driver.BuildOpts{
			ContextPath:   w.workDir,
			Dockerfileath: w.workDir,
			ImageTag:      imageTag,
		}
		buildCmd := w.buildkit.BuildCommand(opts)
		if err := w.executeCommand(buildCmd); err != nil {
			w.log("ERROR: BuildKit build failed: %v", err)
			return w.failJob("BuildKit build failed")
		}
		w.log("BuildKit build and push successful.")
		w.job.ImageTag = imageTag
		if err := w.storage.UpdateJobImageTag(w.job.ID, imageTag); err != nil {
			w.log("ERROR: could not update image tag: %v", err)
			// Don't fail the build for this, just log it
		}
	} else {
		w.log("No Dockerfile found, attempting to auto-detect and generate...")
		if !w.job.BuildConfig.IsAutoBuild {
			w.log("ERROR: Auto-build is not enabled for this job.")
			return w.failJob("No build strategy found (e.g., Dockerfile missing and auto-build disabled)")
		}

		// Detect config and generate Dockerfile content
		detectedConfig, err := autodetect.AutoDetectBuildConfig(w.workDir, w.allowlist)
		if err != nil {
			w.log("ERROR: failed to auto-detect build config: %v", err)
			return w.failJob("failed to auto-detect build config")
		}

		w.log("Auto-detected runtime: %s, version: %s", detectedConfig.Runtime, detectedConfig.Version)
		if detectedConfig.PrebuildCommand != "" {
			w.log("Auto-detected pre-build command: %s", detectedConfig.PrebuildCommand)
		}

		// Write the generated Dockerfile
		if err := os.WriteFile(dockerfilePath, detectedConfig.DockerfileContent, 0644); err != nil {
			w.log("ERROR: failed to write generated Dockerfile: %v", err)
			return w.failJob("failed to write generated Dockerfile")
		}

		w.log("Dockerfile generated successfully, starting BuildKit build...")
		imageTag := w.generateImageTag()
		w.log("Image tag: %s", imageTag)

		opts := driver.BuildOpts{
			ContextPath:   w.workDir,
			Dockerfileath: w.workDir,
			ImageTag:      imageTag,
		}
		buildCmd := w.buildkit.BuildCommand(opts)
		if err := w.executeCommand(buildCmd); err != nil {
			w.log("ERROR: BuildKit build failed: %v", err)
			return w.failJob("BuildKit build failed")
		}
		w.log("BuildKit build and push successful.")
		w.job.ImageTag = imageTag
		if err := w.storage.UpdateJobImageTag(w.job.ID, imageTag); err != nil {
			w.log("ERROR: could not update image tag: %v", err)
		}
	}

	return w.succeedJob()
}

func (w *Worker) failJob(reason string) error {
	log.Printf("Failing job %s: %s", w.job.ID, reason)
	if err := w.storage.UpdateJobStatus(w.job.ID, "failed"); err != nil {
		log.Printf("ERROR: could not update job status to 'failed' for job %s: %v", w.job.ID, err)
	}
	if err := w.apiClient.ReportResult(w.job, "failed", reason); err != nil {
		log.Printf("ERROR: could not report result to backend for job %s: %v", w.job.ID, err)
	}
	return fmt.Errorf("%w: %s", ErrBuildFailed, reason)
}

func (w *Worker) succeedJob() error {
	log.Printf("Succeeding job %s", w.job.ID)
	if err := w.storage.UpdateJobStatus(w.job.ID, "success"); err != nil {
		log.Printf("ERROR: could not update status to 'success' for job %s: %v", w.job.ID, err)
		return err
	}
	if err := w.apiClient.ReportResult(w.job, "success", ""); err != nil {
		log.Printf("ERROR: could not report result to backend for job %s: %v", w.job.ID, err)
		return err
	}
	return nil
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

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		w.streamPipe(stdout)
	}()

	go func() {
		defer wg.Done()
		w.streamPipe(stderr)
	}()

	w.log("Executing: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		return err
	}

	err = cmd.Wait()
	wg.Wait()
	return err
}

func (w *Worker) streamPipe(pipe io.Reader) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		w.log(scanner.Text())
	}
}

func (w *Worker) generateImageTag() string {
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
	return strings.ToLower(strings.ReplaceAll(s, "_", "-"))
}
