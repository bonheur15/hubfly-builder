package autodetect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

type nodePackageJSON struct {
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
}

func DetectRuntime(repoPath string) (string, string) {
	if fileExists(filepath.Join(repoPath, "bun.lock")) { //new version of bun is bun.lock
		return "bun", "1.2" // Simplified version detection
	}
	if fileExists(filepath.Join(repoPath, "package.json")) {
		return "node", "22" // Simplified version detection
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
		return detectNodeCommands(repoPath, allowed)
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

func detectNodeCommands(repoPath string, allowed *allowlist.AllowedCommands) (string, string, string) {
	metadata := loadNodePackageJSON(repoPath)
	packageManager := detectNodePackageManager(repoPath, metadata)
	scripts := map[string]string{}
	if metadata != nil && metadata.Scripts != nil {
		scripts = metadata.Scripts
	}

	prebuildCandidates := nodePrebuildCandidates(repoPath, packageManager)
	buildCandidates := nodeBuildCandidates(packageManager, scripts)
	runCandidates := nodeRunCandidates(packageManager, scripts)

	return pickFirstAllowed(prebuildCandidates, allowed.Prebuild),
		pickFirstAllowed(buildCandidates, allowed.Build),
		pickFirstAllowed(runCandidates, allowed.Run)
}

func loadNodePackageJSON(repoPath string) *nodePackageJSON {
	if repoPath == "" {
		return nil
	}

	path := filepath.Join(repoPath, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var parsed nodePackageJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}
	return &parsed
}

func detectNodePackageManager(repoPath string, metadata *nodePackageJSON) string {
	if metadata != nil {
		pm := strings.ToLower(strings.TrimSpace(metadata.PackageManager))
		switch {
		case strings.HasPrefix(pm, "pnpm@"), pm == "pnpm":
			return "pnpm"
		case strings.HasPrefix(pm, "yarn@"), pm == "yarn":
			return "yarn"
		case strings.HasPrefix(pm, "npm@"), pm == "npm":
			return "npm"
		}
	}

	if repoPath != "" {
		switch {
		case fileExists(filepath.Join(repoPath, "pnpm-lock.yaml")):
			return "pnpm"
		case fileExists(filepath.Join(repoPath, "yarn.lock")):
			return "yarn"
		case fileExists(filepath.Join(repoPath, "package-lock.json")), fileExists(filepath.Join(repoPath, "npm-shrinkwrap.json")):
			return "npm"
		}
	}

	return "npm"
}

func nodePrebuildCandidates(repoPath, packageManager string) []string {
	switch packageManager {
	case "pnpm":
		return []string{"pnpm install"}
	case "yarn":
		return []string{"yarn install"}
	default:
		if repoPath != "" && (fileExists(filepath.Join(repoPath, "package-lock.json")) || fileExists(filepath.Join(repoPath, "npm-shrinkwrap.json"))) {
			return []string{"npm ci", "npm install"}
		}
		return []string{"npm install", "npm ci"}
	}
}

func nodeBuildCandidates(packageManager string, scripts map[string]string) []string {
	scriptNames := make([]string, 0, 4)
	added := make(map[string]struct{})

	addScript := func(name string) {
		if !hasNodeScript(scripts, name) {
			return
		}
		if _, exists := added[name]; exists {
			return
		}
		added[name] = struct{}{}
		scriptNames = append(scriptNames, name)
	}

	addScript("build")
	for _, name := range sortedScriptNames(scripts) {
		lowerName := strings.ToLower(name)
		if strings.HasPrefix(lowerName, "build:") || strings.Contains(lowerName, ":build") {
			addScript(name)
		}
	}

	candidates := make([]string, 0, len(scriptNames)*2)
	for _, name := range scriptNames {
		candidates = append(candidates, nodeScriptCandidates(packageManager, name)...)
	}
	return candidates
}

func nodeRunCandidates(packageManager string, scripts map[string]string) []string {
	candidates := make([]string, 0, 10)
	addedScripts := make(map[string]struct{})

	addScript := func(name string) {
		if !hasNodeScript(scripts, name) {
			return
		}
		if _, exists := addedScripts[name]; exists {
			return
		}
		addedScripts[name] = struct{}{}
		candidates = append(candidates, nodeScriptCandidates(packageManager, name)...)
	}

	for _, name := range []string{"start", "serve", "preview", "dev"} {
		addScript(name)
	}

	for _, name := range sortedScriptNames(scripts) {
		lowerName := strings.ToLower(name)
		if strings.Contains(lowerName, "start") || strings.Contains(lowerName, "serve") || strings.Contains(lowerName, "prod") || strings.Contains(lowerName, "preview") {
			addScript(name)
		}
	}

	if len(addedScripts) == 0 {
		for _, name := range sortedScriptNames(scripts) {
			if isNodeUtilityScript(name) {
				continue
			}
			addScript(name)
			break
		}
	}

	candidates = append(candidates, "node server.js")
	return candidates
}

func sortedScriptNames(scripts map[string]string) []string {
	if len(scripts) == 0 {
		return nil
	}

	names := make([]string, 0, len(scripts))
	for name := range scripts {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

func nodeScriptCandidates(packageManager, script string) []string {
	switch packageManager {
	case "pnpm":
		return []string{"pnpm run " + script, "pnpm " + script}
	case "yarn":
		return []string{"yarn " + script, "yarn run " + script}
	default:
		if script == "start" {
			return []string{"npm start", "npm run start"}
		}
		return []string{"npm run " + script}
	}
}

func isNodeUtilityScript(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))

	switch {
	case name == "build",
		name == "test",
		name == "lint",
		name == "typecheck",
		name == "format",
		name == "clean",
		name == "prepare",
		name == "preinstall",
		name == "postinstall",
		name == "install":
		return true
	case strings.HasPrefix(name, "build:"),
		strings.HasPrefix(name, "test:"),
		strings.HasPrefix(name, "lint:"),
		strings.HasPrefix(name, "typecheck:"),
		strings.HasPrefix(name, "format:"),
		strings.HasPrefix(name, "clean:"):
		return true
	default:
		return false
	}
}

func hasNodeScript(scripts map[string]string, key string) bool {
	if len(scripts) == 0 {
		return false
	}

	value, ok := scripts[key]
	return ok && strings.TrimSpace(value) != ""
}

func detectJavaCommands(repoPath string, allowed *allowlist.AllowedCommands) (string, string, string) {
	isGradle := repoPath != "" && (fileExists(filepath.Join(repoPath, "build.gradle")) || fileExists(filepath.Join(repoPath, "build.gradle.kts")))
	hasMavenWrapper := repoPath != "" && fileExists(filepath.Join(repoPath, "mvnw"))
	hasGradleWrapper := repoPath != "" && fileExists(filepath.Join(repoPath, "gradlew"))

	if isGradle {
		prebuildCandidates := []string{"gradle dependencies"}
		buildCandidates := []string{"gradle build -x test"}
		if hasGradleWrapper {
			prebuildCandidates = []string{"./gradlew dependencies", "gradle dependencies"}
			buildCandidates = []string{"./gradlew build -x test", "gradle build -x test"}
		}

		return pickFirstAllowed(prebuildCandidates, allowed.Prebuild),
			pickFirstAllowed(buildCandidates, allowed.Build),
			pickFirstAllowed([]string{"java -jar build/libs/*.jar"}, allowed.Run)
	}

	prebuildCandidates := []string{"mvn clean"}
	buildCandidates := []string{"mvn install -DskipTests"}
	if hasMavenWrapper {
		prebuildCandidates = []string{"./mvnw clean", "mvn clean"}
		buildCandidates = []string{"./mvnw install -DskipTests", "mvn install -DskipTests"}
	}

	return pickFirstAllowed(prebuildCandidates, allowed.Prebuild),
		pickFirstAllowed(buildCandidates, allowed.Build),
		pickFirstAllowed([]string{"java -jar target/*.jar"}, allowed.Run)
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

func pickFirstAllowed(candidates []string, allowed []string) string {
	for _, candidate := range candidates {
		if allowlist.IsCommandAllowed(candidate, allowed) {
			return candidate
		}
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
