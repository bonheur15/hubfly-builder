package autodetect

import (
	"os"
	"path/filepath"

	"hubfly-builder/internal/allowlist"
)

type BuildConfig struct {
	IsAutoBuild       bool   `json:"isAutoBuild"`
	Runtime           string `json:"runtime"`
	Version           string `json:"version"`
	PrebuildCommand   string `json:"prebuildCommand"`
	BuildCommand      string `json:"buildCommand"`
	RunCommand        string `json:"runCommand"`
	DockerfileContent []byte `json:"dockerfileContent"`
}

func DetectRuntime(repoPath string) (string, string) {
	if fileExists(filepath.Join(repoPath, "bun.lock")) { //new version of bun is bun.lock
		return "bun", "1.2" // Simplified version detection
	}
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
	if fileExists(filepath.Join(repoPath, "pom.xml")) || fileExists(filepath.Join(repoPath, "build.gradle")) || fileExists(filepath.Join(repoPath, "build.gradle.kts")) {
		return "java", "17"
	}
	if fileExists(filepath.Join(repoPath, "index.html")) {
		return "static", "latest"
	}
	return "unknown", ""
}

func DetectCommands(runtime string, allowed *allowlist.AllowedCommands) (string, string, string) {
	return detectCommandsWithPath("", runtime, allowed)
}

func detectCommandsWithPath(repoPath string, runtime string, allowed *allowlist.AllowedCommands) (string, string, string) {
	switch runtime {
	case "static":
		return "", "", ""
	case "node":
		return pickAllowed("npm install", allowed.Prebuild),
			pickAllowed("npm run build", allowed.Build),
			pickAllowed("npm start", allowed.Run)
	case "bun":
		return pickAllowed("bun install", allowed.Prebuild),
			pickAllowed("bun run build", allowed.Build),
			pickAllowed("bun run start", allowed.Run)
	case "python":
		return pickAllowed("pip install -r requirements.txt", allowed.Prebuild),
			pickAllowed("python setup.py build", allowed.Build),
			pickAllowed("python main.py", allowed.Run)
	case "go":
		return pickAllowed("go mod download", allowed.Prebuild),
			pickAllowed("go build ./...", allowed.Build),
			pickAllowed("go run main.go", allowed.Run)
	case "java":
		return detectJavaCommands(repoPath, allowed)
	}
	return "", "", ""
}

func detectJavaCommands(repoPath string, allowed *allowlist.AllowedCommands) (string, string, string) {
	isMaven := repoPath == "" || fileExists(filepath.Join(repoPath, "pom.xml"))
	isGradle := repoPath != "" && (fileExists(filepath.Join(repoPath, "build.gradle")) || fileExists(filepath.Join(repoPath, "build.gradle.kts")))
	
	if isGradle {
		return pickAllowed("gradle dependencies", allowed.Prebuild),
			pickAllowed("gradle build -x test", allowed.Build),
			pickAllowed("java -jar build/libs/*.jar", allowed.Run)
	}
	
	return pickAllowed("mvn clean", allowed.Prebuild),
		pickAllowed("mvn install -DskipTests", allowed.Build),
		pickAllowed("java -jar target/*.jar", allowed.Run)
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

func AutoDetectBuildConfig(repoPath string, allowed *allowlist.AllowedCommands) (BuildConfig, error) {
	runtime, version := DetectRuntime(repoPath)
	prebuild, build, run := detectCommandsWithPath(repoPath, runtime, allowed)

	dockerfileContent, err := GenerateDockerfile(runtime, version, prebuild, build, run)
	if err != nil {
		return BuildConfig{}, err
	}

	return BuildConfig{
		IsAutoBuild:       true,
		Runtime:           runtime,
		Version:           version,
		PrebuildCommand:   prebuild,
		BuildCommand:      build,
		RunCommand:        run,
		DockerfileContent: dockerfileContent,
	}, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
