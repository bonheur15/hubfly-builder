package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	ephemeralBuildKitImage            = "moby/buildkit:buildx-stable-1"
	ephemeralBuildKitPort             = "1234"
	ephemeralBuildKitLabelKey         = "hubfly.role"
	ephemeralBuildKitLabelValue       = "buildkit"
	ephemeralBuildKitReadinessTimeout = 30 * time.Second
	ephemeralBuildKitReadinessPoll    = 500 * time.Millisecond
	defaultHubcellBaseURL             = "http://127.0.0.1:10012"
)

var hubcellDoFunc = http.DefaultClient.Do

type EphemeralBuildKitOpts struct {
	JobID             string
	UserNetwork       string
	Registry          string
	RegistryPlainHTTP bool
	CacheDir          string
	UseLocalCache     bool
	CPULimit          float64
	MemoryMB          int
	UseSoftLimits     bool
}

type EphemeralBuildKit struct {
	ContainerName string
	Addr          string
	UserNetwork   string
	baseURL       string
}

type hubcellNetworkRequest struct {
	Name             string `json:"name"`
	EnableMasquerade bool   `json:"enable_masquerade"`
}

type hubcellRunRequest struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	Entrypoint []string          `json:"entrypoint,omitempty"`
	Command    []string          `json:"command"`
	Labels     map[string]string `json:"labels,omitempty"`
	Security   hubcellSecurity   `json:"security"`
	Network    hubcellNetwork    `json:"network"`
}

type hubcellSecurity struct {
	AddCapabilities  []string `json:"add_capabilities"`
	DropCapabilities []string `json:"drop_capabilities"`
	SeccompProfile   string   `json:"seccomp_profile"`
	NoNewPrivileges  bool     `json:"no_new_privileges"`
}

type hubcellNetwork struct {
	VirtualNetwork string `json:"virtual_network"`
	AllowHost      bool   `json:"allow_host"`
}

type hubcellGetResponse struct {
	Config hubcellCellConfig `json:"config"`
	State  hubcellCellState  `json:"state"`
}

type hubcellListResponse struct {
	Cells []hubcellGetResponse `json:"cells"`
}

type hubcellCellConfig struct {
	ID     string            `json:"id"`
	Labels map[string]string `json:"labels"`
}

type hubcellCellState struct {
	Status string `json:"status"`
	IP     string `json:"ip"`
}

func StartEphemeralBuildKit(ctx context.Context, opts EphemeralBuildKitOpts) (*EphemeralBuildKit, error) {
	jobID := strings.TrimSpace(opts.JobID)
	if jobID == "" {
		return nil, fmt.Errorf("missing job id for ephemeral buildkit")
	}

	userNetwork := strings.TrimSpace(opts.UserNetwork)
	if userNetwork == "" {
		return nil, fmt.Errorf("missing user network for ephemeral buildkit")
	}

	baseURL := hubcellBaseURL()
	if err := ensureHubcellNetwork(ctx, baseURL, userNetwork); err != nil {
		return nil, err
	}

	cellID := "hubfly-buildkit-" + sanitizeContainerName(jobID)
	if err := deleteHubcellCell(ctx, baseURL, cellID); err != nil {
		return nil, err
	}

	if err := runHubcellBuildKitCell(ctx, baseURL, buildHubcellRunRequest(opts, cellID, userNetwork)); err != nil {
		return nil, fmt.Errorf("failed to start ephemeral buildkit cell %q: %w", cellID, err)
	}

	session := &EphemeralBuildKit{
		ContainerName: cellID,
		UserNetwork:   userNetwork,
		baseURL:       baseURL,
	}

	cleanupOnFailure := true
	defer func() {
		if cleanupOnFailure {
			_ = session.Stop()
		}
	}()

	addr, err := resolveBuildKitAddr(ctx, baseURL, cellID)
	if err != nil {
		return nil, err
	}
	session.Addr = addr

	if err := waitForBuildKitReady(ctx, addr); err != nil {
		return nil, err
	}

	cleanupOnFailure = false
	return session, nil
}

func buildHubcellRunRequest(opts EphemeralBuildKitOpts, cellID, userNetwork string) hubcellRunRequest {
	return hubcellRunRequest{
		ID:         cellID,
		Name:       cellID,
		Image:      ephemeralBuildKitImage,
		Entrypoint: []string{"buildkitd"},
		Command:    []string{"--addr", "tcp://0.0.0.0:" + ephemeralBuildKitPort},
		Labels: map[string]string{
			ephemeralBuildKitLabelKey: "buildkit",
			"hubfly.job":              strings.TrimSpace(opts.JobID),
		},
		Security: hubcellSecurity{
			AddCapabilities: []string{
				"CHOWN",
				"SETUID",
				"DAC_OVERRIDE",
				"FOWNER",
				"FSETID",
				"KILL",
				"NET_BIND_SERVICE",
				"NET_RAW",
				"SETFCAP",
				"SETPCAP",
				"SETGID",
				"SYS_CHROOT",
				"SYS_ADMIN",
			},
			DropCapabilities: []string{},
			SeccompProfile:   "unconfined",
			NoNewPrivileges:  false,
		},
		Network: hubcellNetwork{
			VirtualNetwork: userNetwork,
			AllowHost:      true,
		},
	}
}

func hubcellBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("HUBCELL_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultHubcellBaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func ensureHubcellNetwork(ctx context.Context, baseURL, name string) error {
	req := hubcellNetworkRequest{Name: name, EnableMasquerade: true}
	resp, err := hubcellJSON(ctx, http.MethodPost, baseURL+"/v1/networks", req)
	if err != nil {
		return fmt.Errorf("failed to create hubcell network %q: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hubcellHTTPError(resp, "create hubcell network")
	}
	return nil
}

func runHubcellBuildKitCell(ctx context.Context, baseURL string, req hubcellRunRequest) error {
	resp, err := hubcellJSON(ctx, http.MethodPost, baseURL+"/v1/cells/run", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hubcellHTTPError(resp, "run hubcell buildkit cell")
	}
	return nil
}

func (s *EphemeralBuildKit) Stop() error {
	if s == nil || s.ContainerName == "" {
		return nil
	}

	baseURL := strings.TrimRight(s.baseURL, "/")
	if baseURL == "" {
		baseURL = hubcellBaseURL()
	}
	return deleteHubcellCell(context.Background(), baseURL, s.ContainerName)
}

func CleanupOrphanedEphemeralBuildKits() error {
	baseURL := hubcellBaseURL()
	resp, err := hubcellJSON(context.Background(), http.MethodGet, baseURL+"/v1/cells", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hubcellHTTPError(resp, "list hubcell cells")
	}

	var list hubcellListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return fmt.Errorf("decode hubcell cells response: %w", err)
	}
	for _, cell := range list.Cells {
		if cell.Config.Labels[ephemeralBuildKitLabelKey] != ephemeralBuildKitLabelValue {
			continue
		}
		id := strings.TrimSpace(cell.Config.ID)
		if id == "" {
			continue
		}
		if err := deleteHubcellCell(context.Background(), baseURL, id); err != nil {
			return err
		}
	}
	return nil
}

func resolveBuildKitAddr(ctx context.Context, baseURL, cellID string) (string, error) {
	deadline := time.Now().Add(ephemeralBuildKitReadinessTimeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("hubcell buildkit cell %q IP lookup canceled: %w", cellID, err)
		}

		cell, err := getHubcellCell(ctx, baseURL, cellID)
		if err == nil {
			if strings.EqualFold(cell.State.Status, "running") && strings.TrimSpace(cell.State.IP) != "" {
				return "tcp://" + strings.TrimSpace(cell.State.IP) + ":" + ephemeralBuildKitPort, nil
			}
			lastErr = fmt.Errorf("cell status=%q ip=%q", cell.State.Status, cell.State.IP)
		} else {
			lastErr = err
		}
		time.Sleep(ephemeralBuildKitReadinessPoll)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timed out waiting for hubcell IP")
	}
	return "", fmt.Errorf("hubcell buildkit cell %q has no ready IP: %w", cellID, lastErr)
}

func getHubcellCell(ctx context.Context, baseURL, cellID string) (hubcellGetResponse, error) {
	resp, err := hubcellJSON(ctx, http.MethodGet, baseURL+"/v1/cells/"+url.PathEscape(cellID), nil)
	if err != nil {
		return hubcellGetResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hubcellGetResponse{}, hubcellHTTPError(resp, "get hubcell cell")
	}

	var cell hubcellGetResponse
	if err := json.NewDecoder(resp.Body).Decode(&cell); err != nil {
		return hubcellGetResponse{}, fmt.Errorf("decode hubcell cell response: %w", err)
	}
	return cell, nil
}

func deleteHubcellCell(ctx context.Context, baseURL, cellID string) error {
	resp, err := hubcellJSON(ctx, http.MethodDelete, baseURL+"/v1/cells/"+url.PathEscape(cellID)+"?force-delete=true", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hubcellHTTPError(resp, "delete hubcell cell")
	}
	return nil
}

func hubcellJSON(ctx context.Context, method, endpoint string, payload interface{}) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return hubcellDoFunc(req)
}

func hubcellHTTPError(resp *http.Response, action string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("%s failed: status=%s", action, resp.Status)
	}
	return fmt.Errorf("%s failed: status=%s body=%s", action, resp.Status, text)
}

func waitForBuildKitReady(ctx context.Context, addr string) error {
	deadline := time.Now().Add(ephemeralBuildKitReadinessTimeout)
	var lastErr error

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("buildkit daemon at %s readiness canceled: %w", addr, err)
		}
		cmd := exec.CommandContext(ctx, "buildctl", "--addr", addr, "debug", "workers")
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(ephemeralBuildKitReadinessPoll)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("timed out waiting for buildkit readiness")
	}
	return fmt.Errorf("buildkit daemon at %s is not ready: %w", addr, lastErr)
}

func sanitizeContainerName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "job"
	}

	var builder strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			builder.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		case ch == '.', ch == '-', ch == '_':
			builder.WriteRune(ch)
		default:
			builder.WriteByte('-')
		}
	}

	result := strings.Trim(builder.String(), "-_.")
	if result == "" {
		return "job"
	}

	if len(result) > 48 {
		result = result[:48]
	}

	return result
}
