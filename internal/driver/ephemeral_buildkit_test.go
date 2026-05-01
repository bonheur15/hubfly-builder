package driver

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestBuildEphemeralBuildKitRunArgsWithConfig(t *testing.T) {
	opts := EphemeralBuildKitOpts{
		JobID:         "job1",
		UserNetwork:   "user-net",
		UseLocalCache: false,
	}
	args := buildEphemeralBuildKitRunArgs(opts, "hubfly-buildkit-job1", "/tmp/buildkitd.toml", "")
	got := strings.Join(args, " ")

	for _, want := range []string{
		"run -d --rm",
		"--name hubfly-buildkit-job1",
		"--network user-net",
		"-v /tmp/buildkitd.toml:/etc/buildkit/buildkitd.toml:ro",
		"moby/buildkit:buildx-stable-1",
		"--config /etc/buildkit/buildkitd.toml",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in docker args, got %q", want, got)
		}
	}
}

func TestBuildEphemeralBuildKitRunArgsWithoutConfig(t *testing.T) {
	opts := EphemeralBuildKitOpts{
		JobID:         "job1",
		UserNetwork:   "user-net",
		UseLocalCache: false,
	}
	args := buildEphemeralBuildKitRunArgs(opts, "hubfly-buildkit-job1", "", "")
	got := strings.Join(args, " ")

	if strings.Contains(got, "/etc/buildkit/buildkitd.toml") {
		t.Fatalf("did not expect buildkit config mount/flag when no config path is provided: %q", got)
	}
}

func TestStopRemovesAnonymousVolumes(t *testing.T) {
	var got []string
	runDockerCommandContextFunc = func(ctx context.Context, args ...string) (string, error) {
		got = append([]string{}, args...)
		return "", nil
	}
	defer func() {
		runDockerCommandContextFunc = runDockerCommandContextDefault
	}()

	session := &EphemeralBuildKit{ContainerName: "hubfly-buildkit-job1"}
	if err := session.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	want := []string{"rm", "-f", "-v", "hubfly-buildkit-job1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected docker args %v, got %v", want, got)
	}
}
