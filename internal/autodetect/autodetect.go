package autodetect

import (
	"os"
	"path/filepath"

	"hubfly-builder/internal/allowlist"
)

type BuildConfig struct {
	IsAutoBuild     bool   `json:"isAutoBuild"`
	Runtime         string `json:"runtime"`
	Version         string `json:"version"`
	PrebuildCommand string `json:"prebuildCommand"`
	BuildCommand    string `json:"buildCommand"`
	RunCommand      string `json:"runCommand"`
}

func DetectRuntime(repoPath string) (string, string) {
	if fileExists(filepath.Join(repoPath, "package.json")) {
		return "node", "18" // Simplified version detection
	}
	if fileExists(filepath.Join(repoPath, "requirements.txt")) {
		return "python", "3.9" // Simplified version detection
	}
	if fileExists(filepath.Join(repoPath, "go.mod")) {
		return "go", "1.18" // Simplified version detection
	}
	if fileExists(filepath.Join(repoPath, "composer.json")) {
		return "php", "8"
	}
	return "unknown", ""
}

func DetectCommands(runtime string, allowed *allowlist.AllowedCommands) (string, string, string) {
	switch runtime {
	case "node":
		return pickAllowed("npm install", allowed.Prebuild),
			pickAllowed("npm run build", allowed.Build),
			pickAllowed("npm start", allowed.Run)
	case "python":
		return pickAllowed("pip install -r requirements.txt", allowed.Prebuild),
			pickAllowed("python setup.py build", allowed.Build),
			pickAllowed("python main.py", allowed.Run)
	case "go":
		return pickAllowed("go mod download", allowed.Prebuild),
			pickAllowed("go build ./...", allowed.Build),
			pickAllowed("go run main.go", allowed.Run)
	}
	return "", "", ""
}

func pickAllowed(preferred string, allowed []string) string {
	if allowlist.IsCommandAllowed(preferred, allowed) {
		return preferred
	}
	if len(allowed) > 0 {
		return allowed[0]
	}
	return ""
}

func AutoDetectBuildConfig(repoPath string, allowed *allowlist.AllowedCommands) BuildConfig {
	runtime, version := DetectRuntime(repoPath)
	prebuild, build, run := DetectCommands(runtime, allowed)

	return BuildConfig{
		IsAutoBuild:     true,
		Runtime:         runtime,
		Version:         version,
		PrebuildCommand: prebuild,
		BuildCommand:    build,
		RunCommand:      run,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
