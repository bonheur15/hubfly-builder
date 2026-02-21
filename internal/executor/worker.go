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
	"sort"
	"strings"
	"sync"
	"time"

	"hubfly-builder/internal/allowlist"
	"hubfly-builder/internal/api"
	"hubfly-builder/internal/autodetect"
	"hubfly-builder/internal/driver"
	"hubfly-builder/internal/envplan"
	"hubfly-builder/internal/logs"
	"hubfly-builder/internal/storage"
)

var ErrBuildFailed = errors.New("build failed")

type Worker struct {
	job        *storage.BuildJob
	storage    *storage.Storage
	logManager *logs.LogManager
	allowlist  *allowlist.AllowedCommands
	apiClient  *api.Client
	registry   string
	logFile    *os.File
	logWriter  io.Writer
	workDir    string
}

func NewWorker(job *storage.BuildJob, storage *storage.Storage, logManager *logs.LogManager, allowlist *allowlist.AllowedCommands, apiClient *api.Client, registry string) *Worker {
	return &Worker{
		job:        job,
		storage:    storage,
		logManager: logManager,
		allowlist:  allowlist,
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

	if w.job.SourceInfo.Ref != "" {
		w.log("Checking out ref: %s", w.job.SourceInfo.Ref)
		checkoutRefCmd := exec.Command("git", "-C", w.workDir, "checkout", w.job.SourceInfo.Ref)
		if err := w.executeCommand(checkoutRefCmd); err != nil {
			w.log("ERROR: failed to checkout ref %s: %v", w.job.SourceInfo.Ref, err)
			return w.failJob("failed to checkout ref")
		}
	}

	if w.job.SourceInfo.CommitSha != "" {
		w.log("Checking out commit SHA: %s", w.job.SourceInfo.CommitSha)
		checkoutShaCmd := exec.Command("git", "-C", w.workDir, "checkout", w.job.SourceInfo.CommitSha)
		if err := w.executeCommand(checkoutShaCmd); err != nil {
			w.log("ERROR: failed to checkout commit %s: %v", w.job.SourceInfo.CommitSha, err)
			return w.failJob("failed to checkout commit")
		}
	}

	w.log("Repository cloned and checked out successfully.")

	// Determine effective build context directory based on WorkingDir.
	buildContext := w.workDir
	if w.job.SourceInfo.WorkingDir != "" {
		buildContext = filepath.Join(w.workDir, w.job.SourceInfo.WorkingDir)
		w.log("Using working directory: %s", w.job.SourceInfo.WorkingDir)
	}

	if len(w.job.BuildConfig.Env) == 0 && len(w.job.Env) > 0 {
		w.job.BuildConfig.Env = copyStringMap(w.job.Env)
	}

	envResult := envplan.Resolve(buildContext, w.job.BuildConfig.Env, w.job.BuildConfig.EnvOverrides)
	w.job.BuildConfig.ResolvedEnvPlan = envResult.Entries
	w.logResolvedEnvPlan(envResult.Entries)
	if len(w.job.BuildConfig.Env) > 0 {
		if err := w.storage.UpdateJobBuildConfig(w.job.ID, &w.job.BuildConfig); err != nil {
			w.log("WARNING: could not persist resolved env plan: %v", err)
		}
	}

	buildSecrets, cleanupSecrets, err := w.prepareBuildSecrets(envResult.BuildSecrets)
	if err != nil {
		w.log("ERROR: could not prepare build secrets: %v", err)
		return w.failJob("failed to prepare build secrets")
	}
	defer cleanupSecrets()

	requestedNetwork := strings.TrimSpace(w.job.BuildConfig.Network)
	if requestedNetwork == "" {
		w.log("ERROR: no user network provided")
		return w.failJob("no user network provided")
	}

	w.log("Starting ephemeral BuildKit daemon for network: %s", requestedNetwork)
	ephemeralSession, startErr := driver.StartEphemeralBuildKit(driver.EphemeralBuildKitOpts{
		JobID:          w.job.ID,
		UserNetwork:    requestedNetwork,
		ControlNetwork: strings.TrimSpace(os.Getenv("BUILDKIT_CONTROL_NETWORK")),
	})
	if startErr != nil {
		w.log("ERROR: failed to start ephemeral BuildKit daemon: %v", startErr)
		return w.failJob("failed to start ephemeral BuildKit daemon")
	}
	defer func() {
		if stopErr := ephemeralSession.Stop(); stopErr != nil {
			w.log("WARNING: failed to clean up ephemeral BuildKit daemon %s: %v", ephemeralSession.ContainerName, stopErr)
		}
	}()
	w.log("Ephemeral BuildKit ready: container=%s controlNetwork=%s userNetwork=%s addr=%s", ephemeralSession.ContainerName, ephemeralSession.ControlNetwork, ephemeralSession.UserNetwork, ephemeralSession.Addr)
	activeBuildKit := driver.NewBuildKit(ephemeralSession.Addr)

	dockerfilePath := filepath.Join(buildContext, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err == nil {
		w.log("Dockerfile found in context, starting BuildKit build...")

		// Warn if PrebuildCommand is ignored.
		if w.job.BuildConfig.PrebuildCommand != "" {
			w.log("WARNING: PrebuildCommand '%s' is ignored because a Dockerfile was provided. Please include pre-build steps in your Dockerfile.", w.job.BuildConfig.PrebuildCommand)
		}

		imageTag := w.generateImageTag()
		w.log("Image tag: %s", imageTag)

		opts := driver.BuildOpts{
			ContextPath:    buildContext,
			DockerfilePath: buildContext,
			ImageTag:       imageTag,
			BuildArgs:      envResult.BuildArgs,
			Secrets:        buildSecrets,
		}
		buildCmd := activeBuildKit.BuildCommand(opts)
		if err := w.executeCommand(buildCmd); err != nil {
			w.log("ERROR: BuildKit build failed: %v", err)
			return w.failJob("BuildKit build failed")
		}
		w.log("BuildKit build and push successful.")
		w.job.ImageTag = imageTag
		if err := w.storage.UpdateJobImageTag(w.job.ID, imageTag); err != nil {
			w.log("ERROR: could not update image tag: %v", err)
			// Don't fail the build for this, just log it.
		}
	} else {
		w.log("No Dockerfile found in context, attempting to auto-detect and generate...")
		if !w.job.BuildConfig.IsAutoBuild {
			w.log("ERROR: Auto-build is not enabled for this job.")
			return w.failJob("No build strategy found (e.g., Dockerfile missing and auto-build disabled)")
		}

		// Detect config and generate Dockerfile content.
		detectedConfig, err := autodetect.AutoDetectBuildConfig(buildContext, w.allowlist)
		if err != nil {
			w.log("ERROR: failed to auto-detect build config: %v", err)
			return w.failJob("failed to auto-detect build config")
		}

		w.log("Auto-detected runtime: %s, version: %s", detectedConfig.Runtime, detectedConfig.Version)
		if detectedConfig.PrebuildCommand != "" {
			w.log("Auto-detected pre-build command: %s", detectedConfig.PrebuildCommand)
		}

		dockerfileContent, err := autodetect.GenerateDockerfileWithBuildEnv(
			detectedConfig.Runtime,
			detectedConfig.Version,
			detectedConfig.PrebuildCommand,
			detectedConfig.BuildCommand,
			detectedConfig.RunCommand,
			envResult.BuildArgKeys(),
			envResult.BuildSecretKeys(),
		)
		if err != nil {
			w.log("ERROR: failed to generate Dockerfile with env support: %v", err)
			return w.failJob("failed to generate Dockerfile")
		}

		w.job.BuildConfig.Runtime = detectedConfig.Runtime
		w.job.BuildConfig.Version = detectedConfig.Version
		w.job.BuildConfig.PrebuildCommand = detectedConfig.PrebuildCommand
		w.job.BuildConfig.BuildCommand = detectedConfig.BuildCommand
		w.job.BuildConfig.RunCommand = detectedConfig.RunCommand
		w.job.BuildConfig.DockerfileContent = dockerfileContent
		if err := w.storage.UpdateJobBuildConfig(w.job.ID, &w.job.BuildConfig); err != nil {
			w.log("WARNING: could not persist generated Dockerfile metadata: %v", err)
		}

		// Write the generated Dockerfile.
		if err := os.WriteFile(dockerfilePath, dockerfileContent, 0644); err != nil {
			w.log("ERROR: failed to write generated Dockerfile: %v", err)
			return w.failJob("failed to write generated Dockerfile")
		}

		w.log("Dockerfile generated successfully, starting BuildKit build...")
		imageTag := w.generateImageTag()
		w.log("Image tag: %s", imageTag)

		opts := driver.BuildOpts{
			ContextPath:    buildContext,
			DockerfilePath: buildContext,
			ImageTag:       imageTag,
			BuildArgs:      envResult.BuildArgs,
			Secrets:        buildSecrets,
		}
		buildCmd := activeBuildKit.BuildCommand(opts)
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

	w.log("Executing: %s", sanitizeCommandForLog(cmd))
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

func (w *Worker) logResolvedEnvPlan(entries []storage.ResolvedEnvVar) {
	if len(entries) == 0 {
		w.log("Env auto-resolution: no env variables provided")
		return
	}

	for _, entry := range entries {
		w.log("Env auto-resolution: key=%s scope=%s secret=%t reason=%s", entry.Key, entry.Scope, entry.Secret, entry.Reason)
	}
}

func (w *Worker) prepareBuildSecrets(secretValues map[string]string) ([]driver.BuildSecret, func(), error) {
	if len(secretValues) == 0 {
		return nil, func() {}, nil
	}

	secretDir, err := os.MkdirTemp("", fmt.Sprintf("hubfly-builder-secrets-%s-", w.job.ID))
	if err != nil {
		return nil, nil, err
	}

	keys := make([]string, 0, len(secretValues))
	for key := range secretValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	secrets := make([]driver.BuildSecret, 0, len(keys))
	for idx, key := range keys {
		secretPath := filepath.Join(secretDir, fmt.Sprintf("%03d_%s", idx, sanitizeSecretFilename(key)))
		if err := os.WriteFile(secretPath, []byte(secretValues[key]), 0600); err != nil {
			_ = os.RemoveAll(secretDir)
			return nil, nil, err
		}
		secrets = append(secrets, driver.BuildSecret{ID: key, Src: secretPath})
	}

	cleanup := func() {
		_ = os.RemoveAll(secretDir)
	}

	return secrets, cleanup, nil
}

func sanitizeCommandForLog(cmd *exec.Cmd) string {
	if len(cmd.Args) == 0 {
		return cmd.String()
	}

	sanitized := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		sanitized = append(sanitized, redactBuildArg(arg))
	}
	return strings.Join(sanitized, " ")
}

func redactBuildArg(arg string) string {
	idx := strings.Index(arg, "build-arg:")
	if idx == -1 {
		return arg
	}

	start := idx + len("build-arg:")
	eq := strings.Index(arg[start:], "=")
	if eq == -1 {
		return arg
	}

	eq += start
	return arg[:eq+1] + "<redacted>"
}

func sanitizeSecretFilename(value string) string {
	var builder strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			builder.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '-', ch == '_', ch == '.':
			builder.WriteRune(ch)
		default:
			builder.WriteByte('_')
		}
	}

	if builder.Len() == 0 {
		return "secret"
	}
	return builder.String()
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func sanitize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", "-"))
}
