package storage

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestBuildJobUnmarshalAcceptsFractionalCPU(t *testing.T) {
	payload := []byte(`{
		"id":"build_test",
		"projectId":"proj_test",
		"userId":"user_test",
		"sourceType":"git",
		"sourceInfo":{"gitRepository":"https://example.com/repo.git","commitSha":"abc","ref":"main","workingDir":""},
		"buildConfig":{
			"isAutoBuild":true,
			"network":"proj-network",
			"timeoutSeconds":1800,
			"resourceLimits":{"cpu":0.5,"memoryMB":100},
			"env":{},
			"envOverrides":{}
		}
	}`)

	var job BuildJob
	if err := json.Unmarshal(payload, &job); err != nil {
		t.Fatalf("expected fractional cpu payload to unmarshal: %v", err)
	}

	if math.Abs(job.BuildConfig.ResourceLimits.CPU-0.5) > 1e-9 {
		t.Fatalf("expected cpu 0.5, got %v", job.BuildConfig.ResourceLimits.CPU)
	}
	if job.BuildConfig.ResourceLimits.MemoryMB != 100 {
		t.Fatalf("expected memory 100, got %d", job.BuildConfig.ResourceLimits.MemoryMB)
	}
}

func TestBuildJobUnmarshalAcceptsIntegerCPU(t *testing.T) {
	payload := []byte(`{
		"id":"build_test",
		"projectId":"proj_test",
		"userId":"user_test",
		"sourceType":"git",
		"sourceInfo":{"gitRepository":"https://example.com/repo.git","commitSha":"abc","ref":"main","workingDir":""},
		"buildConfig":{
			"isAutoBuild":true,
			"network":"proj-network",
			"timeoutSeconds":1800,
			"resourceLimits":{"cpu":2,"memoryMB":256},
			"env":{},
			"envOverrides":{}
		}
	}`)

	var job BuildJob
	if err := json.Unmarshal(payload, &job); err != nil {
		t.Fatalf("expected integer cpu payload to unmarshal: %v", err)
	}

	if math.Abs(job.BuildConfig.ResourceLimits.CPU-2.0) > 1e-9 {
		t.Fatalf("expected cpu 2.0, got %v", job.BuildConfig.ResourceLimits.CPU)
	}
	if job.BuildConfig.ResourceLimits.MemoryMB != 256 {
		t.Fatalf("expected memory 256, got %d", job.BuildConfig.ResourceLimits.MemoryMB)
	}
}

func TestBuildConfigCustomDockerfileAcceptsPlainJSONText(t *testing.T) {
	payload := []byte(`{"customDockerfile":"FROM busybox\nCMD [\"true\"]\n"}`)

	var cfg BuildConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		t.Fatalf("failed to unmarshal build config: %v", err)
	}

	content := string(cfg.CustomDockerfileBytes())
	if !strings.Contains(content, "FROM busybox") {
		t.Fatalf("expected custom Dockerfile bytes to contain request content, got %q", content)
	}
}
