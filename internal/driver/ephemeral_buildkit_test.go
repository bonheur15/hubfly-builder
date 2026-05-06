package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBuildHubcellRunRequestUsesExpectedSecurityAndNetwork(t *testing.T) {
	req := buildHubcellRunRequest(EphemeralBuildKitOpts{JobID: "job1"}, "hubfly-buildkit-job1", "project-net")

	if req.ID != "hubfly-buildkit-job1" || req.Name != "hubfly-buildkit-job1" {
		t.Fatalf("unexpected cell id/name: %#v", req)
	}
	if req.Image != ephemeralBuildKitImage {
		t.Fatalf("expected image %q, got %q", ephemeralBuildKitImage, req.Image)
	}
	if strings.Join(req.Entrypoint, " ") != "buildkitd" {
		t.Fatalf("expected buildkitd entrypoint, got %v", req.Entrypoint)
	}
	if strings.Join(req.Command, " ") != "--addr tcp://0.0.0.0:1234" {
		t.Fatalf("unexpected command: %v", req.Command)
	}
	if req.Labels[ephemeralBuildKitLabelKey] != ephemeralBuildKitLabelValue {
		t.Fatalf("expected buildkit label, got %v", req.Labels)
	}
	if req.Network.VirtualNetwork != "project-net" || !req.Network.AllowHost {
		t.Fatalf("expected project network with host access, got %#v", req.Network)
	}
	if req.Security.SeccompProfile != "unconfined" || req.Security.NoNewPrivileges {
		t.Fatalf("unexpected security settings: %#v", req.Security)
	}
	for _, want := range []string{"CHOWN", "DAC_OVERRIDE", "NET_BIND_SERVICE", "NET_RAW", "SYS_CHROOT", "SYS_ADMIN"} {
		if !contains(req.Security.AddCapabilities, want) {
			t.Fatalf("expected capability %q in %v", want, req.Security.AddCapabilities)
		}
	}
}

func TestStopDeletesHubcellCell(t *testing.T) {
	var gotMethod, gotPath string
	hubcellDoFunc = func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.RequestURI()
		return hubcellTestResponse(http.StatusNoContent, ""), nil
	}
	defer func() {
		hubcellDoFunc = http.DefaultClient.Do
	}()

	session := &EphemeralBuildKit{ContainerName: "hubfly-buildkit-job1", baseURL: "http://hubcell.local"}
	if err := session.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Fatalf("expected DELETE, got %s", gotMethod)
	}
	if gotPath != "/v1/cells/hubfly-buildkit-job1?force-delete=true" {
		t.Fatalf("unexpected delete path: %s", gotPath)
	}
}

func TestCleanupOrphanedEphemeralBuildKitsDeletesBuildKitCells(t *testing.T) {
	t.Setenv("HUBCELL_BASE_URL", "http://hubcell.local")
	deleted := make([]string, 0)
	hubcellDoFunc = func(req *http.Request) (*http.Response, error) {
		switch req.Method + " " + req.URL.Path {
		case "GET /v1/cells":
			return hubcellTestJSON(http.StatusOK, hubcellListResponse{
				Cells: []hubcellGetResponse{
					{Config: hubcellCellConfig{ID: "hubfly-buildkit-a", Labels: map[string]string{ephemeralBuildKitLabelKey: ephemeralBuildKitLabelValue}}},
					{Config: hubcellCellConfig{ID: "app-cell", Labels: map[string]string{"hubfly.role": "app"}}},
					{Config: hubcellCellConfig{ID: "hubfly-buildkit-b", Labels: map[string]string{ephemeralBuildKitLabelKey: ephemeralBuildKitLabelValue}}},
				},
			}), nil
		default:
			if req.Method == http.MethodDelete {
				deleted = append(deleted, strings.TrimPrefix(req.URL.Path, "/v1/cells/"))
				return hubcellTestResponse(http.StatusNoContent, ""), nil
			}
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	}
	defer func() {
		hubcellDoFunc = http.DefaultClient.Do
	}()

	if err := CleanupOrphanedEphemeralBuildKits(); err != nil {
		t.Fatalf("CleanupOrphanedEphemeralBuildKits returned error: %v", err)
	}

	if strings.Join(deleted, ",") != "hubfly-buildkit-a,hubfly-buildkit-b" {
		t.Fatalf("unexpected deleted cells: %v", deleted)
	}
}

func TestEnsureHubcellNetworkCreatesMasqueradedNetwork(t *testing.T) {
	var payload hubcellNetworkRequest
	hubcellDoFunc = func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/v1/networks" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		return hubcellTestResponse(http.StatusCreated, ""), nil
	}
	defer func() {
		hubcellDoFunc = http.DefaultClient.Do
	}()

	if err := ensureHubcellNetwork(context.Background(), "http://hubcell.local", "project-net"); err != nil {
		t.Fatalf("ensureHubcellNetwork returned error: %v", err)
	}
	if payload.Name != "project-net" || !payload.EnableMasquerade {
		t.Fatalf("unexpected network payload: %#v", payload)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hubcellTestJSON(status int, value interface{}) *http.Response {
	data, _ := json.Marshal(value)
	return hubcellTestResponse(status, string(data))
}

func hubcellTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}
