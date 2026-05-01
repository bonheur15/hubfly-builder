package dockerfileparams

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageInjectsArgsAndEnvIntoRepoDockerfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte("# syntax=docker/dockerfile:1\nFROM alpine AS build\nRUN echo ok\nFROM scratch\n"), 0644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	content, err := Stage(path, map[string]string{"BUILD_VERSION": "1"}, map[string]string{"APP_ENV": "prod"})
	if err != nil {
		t.Fatalf("Stage returned error: %v", err)
	}

	dockerfile := string(content)
	if strings.Index(dockerfile, "# syntax=docker/dockerfile:1") > strings.Index(dockerfile, "ARG APP_ENV") {
		t.Fatalf("expected parser directive to remain before injected args:\n%s", dockerfile)
	}
	if strings.Count(dockerfile, "ARG BUILD_VERSION") != 3 {
		t.Fatalf("expected global and per-stage BUILD_VERSION args, got:\n%s", dockerfile)
	}
	if strings.Count(dockerfile, "ENV APP_ENV=${APP_ENV}") != 2 {
		t.Fatalf("expected APP_ENV in each stage, got:\n%s", dockerfile)
	}
}

func TestStageRejectsInvalidDockerfileIdentifier(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	if _, err := Stage(path, map[string]string{"BAD-NAME": "1"}, nil); err == nil {
		t.Fatalf("expected invalid identifier error")
	}
}
