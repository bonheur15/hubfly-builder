package uploadserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Server struct {
	callbackURL string
	registry    string
	httpClient  *http.Client
}

type callbackPayload struct {
	ID              string  `json:"id"`
	Status          string  `json:"status"`
	StartedAt       string  `json:"startedAt,omitempty"`
	FinishedAt      string  `json:"finishedAt,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	ImageTag        string  `json:"imageTag,omitempty"`
	Error           string  `json:"error,omitempty"`
	UploadToken     string  `json:"uploadToken,omitempty"`
}

func NewServer(callbackURL, registry string) *Server {
	return &Server{
		callbackURL: strings.TrimSpace(callbackURL),
		registry:    strings.TrimSpace(registry),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/image-upload", s.handleImageUpload)
	mux.HandleFunc("/healthz", s.handleHealth)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleImageUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	startedAt := time.Now().UTC()
	buildID := strings.TrimSpace(r.Header.Get("X-Hubfly-Build-Id"))
	sourceImage := strings.TrimSpace(r.Header.Get("X-Hubfly-Source-Image"))
	uploadToken := strings.TrimSpace(r.Header.Get("X-Hubfly-Upload-Token"))
	if buildID == "" || sourceImage == "" || uploadToken == "" {
		http.Error(w, "missing upload headers", http.StatusBadRequest)
		return
	}

	if err := s.reportCallback(callbackPayload{
		ID:          buildID,
		Status:      "building",
		StartedAt:   startedAt.Format(time.RFC3339),
		UploadToken: uploadToken,
	}); err != nil {
		log.Printf("WARN: failed to report building status for %s: %v", buildID, err)
	}

	archivePath, cleanup, err := writeRequestBodyToTemp(r.Body, buildID)
	if err != nil {
		s.failUpload(w, buildID, uploadToken, startedAt, fmt.Errorf("write upload archive: %w", err))
		return
	}
	defer cleanup()

	if output, err := runCommand("docker", "load", "-i", archivePath); err != nil {
		s.failUpload(w, buildID, uploadToken, startedAt, fmt.Errorf("docker load failed: %s", sanitizeOutput(output, err)))
		return
	}

	if output, err := runCommand("docker", "image", "inspect", sourceImage); err != nil {
		s.failUpload(w, buildID, uploadToken, startedAt, fmt.Errorf("loaded image %q not found: %s", sourceImage, sanitizeOutput(output, err)))
		return
	}

	imageTag := s.generateImageTag(buildID)
	if output, err := runCommand("docker", "tag", sourceImage, imageTag); err != nil {
		s.failUpload(w, buildID, uploadToken, startedAt, fmt.Errorf("docker tag failed: %s", sanitizeOutput(output, err)))
		return
	}

	defer func() {
		_, _ = runCommand("docker", "image", "rm", "-f", imageTag)
		_, _ = runCommand("docker", "image", "rm", "-f", sourceImage)
	}()

	if output, err := runCommand("docker", "push", imageTag); err != nil {
		s.failUpload(w, buildID, uploadToken, startedAt, fmt.Errorf("docker push failed: %s", sanitizeOutput(output, err)))
		return
	}

	finishedAt := time.Now().UTC()
	if err := s.reportCallback(callbackPayload{
		ID:              buildID,
		Status:          "success",
		StartedAt:       startedAt.Format(time.RFC3339),
		FinishedAt:      finishedAt.Format(time.RFC3339),
		DurationSeconds: finishedAt.Sub(startedAt).Seconds(),
		ImageTag:        imageTag,
		UploadToken:     uploadToken,
	}); err != nil {
		http.Error(w, fmt.Sprintf("callback failed: %v", err), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"buildId":  buildID,
		"imageTag": imageTag,
	})
}

func (s *Server) failUpload(
	w http.ResponseWriter,
	buildID, uploadToken string,
	startedAt time.Time,
	err error,
) {
	log.Printf("ERROR: image upload failed for %s: %v", buildID, err)
	finishedAt := time.Now().UTC()
	if callbackErr := s.reportCallback(callbackPayload{
		ID:              buildID,
		Status:          "failed",
		StartedAt:       startedAt.Format(time.RFC3339),
		FinishedAt:      finishedAt.Format(time.RFC3339),
		DurationSeconds: finishedAt.Sub(startedAt).Seconds(),
		Error:           err.Error(),
		UploadToken:     uploadToken,
	}); callbackErr != nil {
		log.Printf("WARN: failed to report failed upload callback for %s: %v", buildID, callbackErr)
	}
	http.Error(w, err.Error(), http.StatusBadGateway)
}

func (s *Server) reportCallback(payload callbackPayload) error {
	if s.callbackURL == "" {
		return fmt.Errorf("callback url is not configured")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, s.callbackURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("callback status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (s *Server) generateImageTag(buildID string) string {
	ts := time.Now().UTC().Format("20060102T150405Z")
	buildID = sanitizeImageComponent(buildID)
	if buildID == "" {
		buildID = "upload"
	}
	repository := fmt.Sprintf("%s/hubfly-cli-upload", s.registry)
	return fmt.Sprintf("%s:%s-%s", repository, buildID, ts)
}

func writeRequestBodyToTemp(body io.ReadCloser, buildID string) (string, func(), error) {
	defer body.Close()

	tmpDir, err := os.MkdirTemp("", "hubfly-upload-"+sanitizeImageComponent(buildID)+"-")
	if err != nil {
		return "", nil, err
	}

	path := filepath.Join(tmpDir, "image.tar")
	file, err := os.Create(path)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", nil, err
	}

	if _, err := io.Copy(file, body); err != nil {
		_ = file.Close()
		_ = os.RemoveAll(tmpDir)
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", nil, err
	}

	return path, func() {
		_ = os.RemoveAll(tmpDir)
	}, nil
}

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func sanitizeOutput(output string, err error) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return err.Error()
	}
	return output
}

func sanitizeImageComponent(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}

	var builder strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '-', ch == '.', ch == '_':
			builder.WriteRune(ch)
		default:
			builder.WriteByte('-')
		}
	}

	return strings.Trim(builder.String(), "-._")
}
