package driver

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	ephemeralBuildKitImage            = "moby/buildkit:buildx-stable-1"
	ephemeralBuildKitPort             = "1234"
	ephemeralBuildKitLabelKey         = "hubfly.builder.ephemeral"
	ephemeralBuildKitLabelValue       = "true"
	defaultEphemeralControlNetwork    = "bridge"
	ephemeralBuildKitReadinessTimeout = 30 * time.Second
	ephemeralBuildKitReadinessPoll    = 500 * time.Millisecond
)

type EphemeralBuildKitOpts struct {
	JobID          string
	UserNetwork    string
	ControlNetwork string
}

type EphemeralBuildKit struct {
	ContainerName  string
	Addr           string
	UserNetwork    string
	ControlNetwork string
}

func StartEphemeralBuildKit(opts EphemeralBuildKitOpts) (*EphemeralBuildKit, error) {
	jobID := strings.TrimSpace(opts.JobID)
	if jobID == "" {
		return nil, fmt.Errorf("missing job id for ephemeral buildkit")
	}

	userNetwork := strings.TrimSpace(opts.UserNetwork)
	if userNetwork == "" {
		return nil, fmt.Errorf("missing user network for ephemeral buildkit")
	}

	controlNetwork, err := resolveControlNetwork(opts.ControlNetwork)
	if err != nil {
		return nil, err
	}

	if err := ensureDockerNetworkExists(controlNetwork); err != nil {
		return nil, err
	}
	if userNetwork != controlNetwork {
		if err := ensureDockerNetworkExists(userNetwork); err != nil {
			return nil, err
		}
	}

	containerName := "hubfly-buildkit-" + sanitizeContainerName(jobID)
	if err := forceRemoveContainer(containerName); err != nil {
		return nil, err
	}

	_, err = runDockerCommand(
		"run", "-d", "--rm",
		"--name", containerName,
		"--privileged",
		"--label", ephemeralBuildKitLabelKey+"="+ephemeralBuildKitLabelValue,
		"--network", controlNetwork,
		ephemeralBuildKitImage,
		"--addr", "tcp://0.0.0.0:"+ephemeralBuildKitPort,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start ephemeral buildkit container %q: %w", containerName, err)
	}

	session := &EphemeralBuildKit{
		ContainerName:  containerName,
		UserNetwork:    userNetwork,
		ControlNetwork: controlNetwork,
	}

	cleanupOnFailure := true
	defer func() {
		if cleanupOnFailure {
			_ = session.Stop()
		}
	}()

	if userNetwork != controlNetwork {
		output, connectErr := runDockerCommand("network", "connect", userNetwork, containerName)
		if connectErr != nil && !strings.Contains(strings.ToLower(output), "already exists") {
			return nil, fmt.Errorf("failed to connect container %q to network %q: %w", containerName, userNetwork, connectErr)
		}
	}

	addr, err := resolveBuildKitAddr(containerName, controlNetwork)
	if err != nil {
		return nil, err
	}
	session.Addr = addr

	if err := waitForBuildKitReady(addr); err != nil {
		return nil, err
	}

	cleanupOnFailure = false
	return session, nil
}

func (s *EphemeralBuildKit) Stop() error {
	if s == nil || s.ContainerName == "" {
		return nil
	}

	output, err := runDockerCommand("rm", "-f", s.ContainerName)
	if err != nil && !isNoSuchContainerError(output) {
		return fmt.Errorf("failed to remove container %q: %w", s.ContainerName, err)
	}
	return nil
}

func CleanupOrphanedEphemeralBuildKits() error {
	output, err := runDockerCommand("ps", "-aq", "--filter", "label="+ephemeralBuildKitLabelKey+"="+ephemeralBuildKitLabelValue)
	if err != nil {
		return err
	}

	ids := splitLines(output)
	for _, id := range ids {
		removeOut, removeErr := runDockerCommand("rm", "-f", id)
		if removeErr != nil && !isNoSuchContainerError(removeOut) {
			return fmt.Errorf("failed to remove stale buildkit container %q: %w", id, removeErr)
		}
	}

	return nil
}

func resolveBuildKitAddr(containerName, controlNetwork string) (string, error) {
	ip, err := inspectContainerIPAddress(containerName, controlNetwork)
	if err != nil {
		return "", err
	}
	if ip == "" {
		return "", fmt.Errorf("container %q has no IP on network %q", containerName, controlNetwork)
	}
	return "tcp://" + ip + ":" + ephemeralBuildKitPort, nil
}

func inspectContainerIPAddress(containerName, network string) (string, error) {
	format := fmt.Sprintf(`{{with index .NetworkSettings.Networks %q}}{{.IPAddress}}{{end}}`, network)
	output, err := runDockerCommand("inspect", "--format", format, containerName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect IP for container %q on network %q: %w", containerName, network, err)
	}
	return strings.TrimSpace(output), nil
}

func resolveControlNetwork(configured string) (string, error) {
	network := strings.TrimSpace(configured)
	if network != "" {
		return network, nil
	}

	if !runningInContainer() {
		return defaultEphemeralControlNetwork, nil
	}

	networks, err := detectCurrentContainerNetworks()
	if err != nil {
		return "", fmt.Errorf("failed to auto-detect control network; set BUILDKIT_CONTROL_NETWORK: %w", err)
	}
	if len(networks) == 0 {
		return "", fmt.Errorf("no container networks detected; set BUILDKIT_CONTROL_NETWORK")
	}

	for _, candidate := range networks {
		if candidate == "host" || candidate == "none" {
			continue
		}
		return candidate, nil
	}

	return "", fmt.Errorf("detected only unsupported networks (%s); set BUILDKIT_CONTROL_NETWORK", strings.Join(networks, ","))
}

func detectCurrentContainerNetworks() ([]string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	output, err := runDockerCommand("inspect", "--format", `{{range $key, $_ := .NetworkSettings.Networks}}{{println $key}}{{end}}`, hostname)
	if err != nil {
		return nil, err
	}

	names := splitLines(output)
	unique := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		unique[name] = struct{}{}
	}

	out := make([]string, 0, len(unique))
	for name := range unique {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func ensureDockerNetworkExists(name string) error {
	_, err := runDockerCommand("network", "inspect", name)
	if err != nil {
		return fmt.Errorf("docker network %q not found or inaccessible: %w", name, err)
	}
	return nil
}

func waitForBuildKitReady(addr string) error {
	deadline := time.Now().Add(ephemeralBuildKitReadinessTimeout)
	var lastErr error

	for time.Now().Before(deadline) {
		cmd := exec.Command("buildctl", "--addr", addr, "debug", "workers")
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

func forceRemoveContainer(name string) error {
	output, err := runDockerCommand("rm", "-f", name)
	if err != nil && !isNoSuchContainerError(output) {
		return fmt.Errorf("failed to remove existing container %q: %w", name, err)
	}
	return nil
}

func isNoSuchContainerError(output string) bool {
	text := strings.ToLower(output)
	return strings.Contains(text, "no such container") || strings.Contains(text, "no such object")
}

func runningInContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func splitLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func runDockerCommand(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if trimmed == "" {
			return "", fmt.Errorf("docker %s failed: %w", strings.Join(args, " "), err)
		}
		return trimmed, fmt.Errorf("docker %s failed: %w: %s", strings.Join(args, " "), err, trimmed)
	}
	return trimmed, nil
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
