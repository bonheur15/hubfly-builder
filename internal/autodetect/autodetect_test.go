package autodetect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hubfly-builder/internal/allowlist"
)

func touchFile(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to create %s: %v", name, err)
	}
}

func writePackageJSON(t *testing.T, dir string, scripts map[string]string, packageManager string) {
	t.Helper()

	payload := map[string]interface{}{
		"name": "sample-app",
	}
	if len(scripts) > 0 {
		payload["scripts"] = scripts
	}
	if packageManager != "" {
		payload["packageManager"] = packageManager
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), data, 0o644); err != nil {
		t.Fatalf("failed to write package.json: %v", err)
	}
}

func javaAllowedCommands() *allowlist.AllowedCommands {
	return &allowlist.AllowedCommands{
		Prebuild: []string{
			"mvn clean",
			"./mvnw clean",
			"gradle dependencies",
			"./gradlew dependencies",
		},
		Build: []string{
			"mvn install -DskipTests",
			"./mvnw install -DskipTests",
			"gradle build -x test",
			"./gradlew build -x test",
		},
		Run: []string{
			"java -jar target/*.jar",
			"java -jar build/libs/*.jar",
		},
	}
}

func nodeAllowedCommands() *allowlist.AllowedCommands {
	return &allowlist.AllowedCommands{
		Prebuild: []string{
			"npm ci",
			"npm install",
			"yarn install",
			"pnpm install",
		},
		Build: []string{
			"npm run build",
			"npm run build:*",
			"yarn build",
			"yarn run build",
			"yarn run build:*",
			"yarn build:*",
			"pnpm run build",
			"pnpm run build:*",
			"pnpm build",
			"pnpm build:*",
		},
		Run: []string{
			"npm start",
			"npm run start",
			"npm run *",
			"npm run serve",
			"npm run preview",
			"npm run dev",
			"yarn start",
			"yarn run start",
			"yarn run *",
			"yarn serve",
			"yarn preview",
			"yarn dev",
			"yarn run serve",
			"yarn run preview",
			"yarn run dev",
			"pnpm start",
			"pnpm run start",
			"pnpm run *",
			"pnpm run serve",
			"pnpm run preview",
			"pnpm run dev",
			"pnpm serve",
			"pnpm preview",
			"pnpm dev",
			"node server.js",
		},
	}
}

func pythonAllowedCommands() *allowlist.AllowedCommands {
	return &allowlist.AllowedCommands{
		Prebuild: []string{
			"pip install -r requirements.txt",
			"pip install pipenv && pipenv install --system --deploy",
			"pip install .",
		},
		Build: []string{
			"python setup.py build",
		},
		Run: []string{
			"python main.py",
			"python app.py",
			"python server.py",
			"python run.py",
			"python manage.py",
			"python manage.py runserver 0.0.0.0:${PORT:-8000}",
			"python *.py",
			"python -m *",
			"uvicorn *:* --host 0.0.0.0 --port ${PORT:-8000}",
			"uvicorn *:app --host 0.0.0.0 --port ${PORT:-8000}",
			"uvicorn *:application --host 0.0.0.0 --port ${PORT:-8000}",
			"gunicorn *:* --bind 0.0.0.0:${PORT:-8000}",
			"gunicorn *:app --bind 0.0.0.0:${PORT:-8000}",
			"gunicorn *:application --bind 0.0.0.0:${PORT:-8000}",
			"flask run --host=0.0.0.0 --port=${PORT:-8000}",
		},
	}
}

func goAllowedCommands() *allowlist.AllowedCommands {
	return &allowlist.AllowedCommands{
		Prebuild: []string{
			"go work sync",
			"go mod download",
		},
		Build: []string{
			"go build -o app .",
			"go build -o app ./cmd/*",
			"go build -o app ./*",
			"go build ./...",
		},
		Run: []string{
			"./app",
			"go run .",
			"go run ./cmd/*",
			"go run ./*",
			"go run main.go",
		},
	}
}

func TestAutoDetectBuildConfigJavaMaven(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "pom.xml")

	cfg, err := AutoDetectBuildConfig(repo, javaAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.Runtime != "java" {
		t.Fatalf("expected runtime java, got %q", cfg.Runtime)
	}
	if cfg.PrebuildCommand != "mvn clean" {
		t.Fatalf("expected maven prebuild command, got %q", cfg.PrebuildCommand)
	}
	if cfg.BuildCommand != "mvn install -DskipTests" {
		t.Fatalf("expected maven build command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "java -jar target/*.jar" {
		t.Fatalf("expected maven run command, got %q", cfg.RunCommand)
	}
	dockerfile := string(cfg.DockerfileContent)
	if !strings.Contains(dockerfile, "FROM maven:3.9-eclipse-temurin-17") {
		t.Fatalf("expected maven base image in Dockerfile, got:\n%s", dockerfile)
	}
}

func TestAutoDetectBuildConfigJavaGradleWrapper(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "build.gradle")
	touchFile(t, repo, "gradlew")

	cfg, err := AutoDetectBuildConfig(repo, javaAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.PrebuildCommand != "./gradlew dependencies" {
		t.Fatalf("expected gradle wrapper prebuild command, got %q", cfg.PrebuildCommand)
	}
	if cfg.BuildCommand != "./gradlew build -x test" {
		t.Fatalf("expected gradle wrapper build command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "java -jar build/libs/*.jar" {
		t.Fatalf("expected gradle run command, got %q", cfg.RunCommand)
	}
	dockerfile := string(cfg.DockerfileContent)
	if !strings.Contains(dockerfile, "FROM gradle:8-jdk17") {
		t.Fatalf("expected gradle base image in Dockerfile, got:\n%s", dockerfile)
	}
}

func TestGenerateDockerfileJavaFallbackBase(t *testing.T) {
	content, err := GenerateDockerfile("java", "21", "", "", "java -jar app.jar")
	if err != nil {
		t.Fatalf("GenerateDockerfile returned error: %v", err)
	}

	if !strings.Contains(string(content), "FROM eclipse-temurin:21-jdk") {
		t.Fatalf("expected temurin base image, got:\n%s", string(content))
	}
}

func TestAutoDetectBuildConfigNodeUsesNpmCIAndScripts(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, repo, map[string]string{
		"build": "webpack",
		"start": "node dist/server.js",
	}, "")
	touchFile(t, repo, "package-lock.json")

	cfg, err := AutoDetectBuildConfig(repo, nodeAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.Runtime != "node" {
		t.Fatalf("expected runtime node, got %q", cfg.Runtime)
	}
	if cfg.PrebuildCommand != "npm ci" {
		t.Fatalf("expected npm ci prebuild command, got %q", cfg.PrebuildCommand)
	}
	if cfg.BuildCommand != "npm run build" {
		t.Fatalf("expected npm run build command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "npm start" {
		t.Fatalf("expected npm start command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigNodePnpmServeNoBuild(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, repo, map[string]string{
		"serve": "node server.js",
	}, "pnpm@9.0.0")

	cfg, err := AutoDetectBuildConfig(repo, nodeAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.PrebuildCommand != "pnpm install" {
		t.Fatalf("expected pnpm install prebuild command, got %q", cfg.PrebuildCommand)
	}
	if cfg.BuildCommand != "" {
		t.Fatalf("expected empty build command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "pnpm run serve" {
		t.Fatalf("expected pnpm run serve command, got %q", cfg.RunCommand)
	}
	dockerfile := string(cfg.DockerfileContent)
	if strings.Contains(dockerfile, "RUN pnpm run build") {
		t.Fatalf("did not expect build RUN line in Dockerfile:\n%s", dockerfile)
	}
}

func TestAutoDetectBuildConfigNodeFallbackToServerJS(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, repo, nil, "")

	cfg, err := AutoDetectBuildConfig(repo, nodeAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.PrebuildCommand != "npm install" && cfg.PrebuildCommand != "npm ci" {
		t.Fatalf("expected npm install or npm ci prebuild command, got %q", cfg.PrebuildCommand)
	}
	if cfg.BuildCommand != "" {
		t.Fatalf("expected empty build command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "node server.js" {
		t.Fatalf("expected node server.js command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigNodeCustomStartScript(t *testing.T) {
	repo := t.TempDir()
	writePackageJSON(t, repo, map[string]string{
		"start:prod": "node dist/server.js",
	}, "")
	touchFile(t, repo, "package-lock.json")

	cfg, err := AutoDetectBuildConfig(repo, nodeAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.PrebuildCommand != "npm ci" {
		t.Fatalf("expected npm ci prebuild command, got %q", cfg.PrebuildCommand)
	}
	if cfg.BuildCommand != "" {
		t.Fatalf("expected empty build command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "npm run start:prod" {
		t.Fatalf("expected npm run start:prod command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigPythonDjango(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "requirements.txt")
	touchFile(t, repo, "manage.py")

	cfg, err := AutoDetectBuildConfig(repo, pythonAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.Runtime != "python" {
		t.Fatalf("expected runtime python, got %q", cfg.Runtime)
	}
	if cfg.PrebuildCommand != "pip install -r requirements.txt" {
		t.Fatalf("expected pip install from requirements, got %q", cfg.PrebuildCommand)
	}
	if cfg.RunCommand != "python manage.py runserver 0.0.0.0:${PORT:-8000}" {
		t.Fatalf("expected django runserver command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigPythonFastAPI(t *testing.T) {
	repo := t.TempDir()
	mainPy := `from fastapi import FastAPI

api = FastAPI()
`
	if err := os.WriteFile(filepath.Join(repo, "main.py"), []byte(mainPy), 0o644); err != nil {
		t.Fatalf("failed to write main.py: %v", err)
	}
	touchFile(t, repo, "pyproject.toml")

	cfg, err := AutoDetectBuildConfig(repo, pythonAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.PrebuildCommand != "pip install ." {
		t.Fatalf("expected pip install . prebuild, got %q", cfg.PrebuildCommand)
	}
	if cfg.RunCommand != "uvicorn main:api --host 0.0.0.0 --port ${PORT:-8000}" {
		t.Fatalf("expected uvicorn fastapi command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigPythonModuleEntrypoint(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "myapp"), 0o755); err != nil {
		t.Fatalf("failed to create package dir: %v", err)
	}
	touchFile(t, filepath.Join(repo, "myapp"), "__main__.py")
	touchFile(t, repo, "pyproject.toml")

	cfg, err := AutoDetectBuildConfig(repo, pythonAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.RunCommand != "python -m myapp" {
		t.Fatalf("expected python -m module command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigPythonPipfile(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "Pipfile")
	touchFile(t, repo, "app.py")

	cfg, err := AutoDetectBuildConfig(repo, pythonAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.PrebuildCommand != "pip install pipenv && pipenv install --system --deploy" {
		t.Fatalf("expected pipenv prebuild command, got %q", cfg.PrebuildCommand)
	}
	if cfg.RunCommand != "python app.py" {
		t.Fatalf("expected python app.py run command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigPythonWSGI(t *testing.T) {
	repo := t.TempDir()
	wsgiPy := `application = object()
`
	if err := os.WriteFile(filepath.Join(repo, "wsgi.py"), []byte(wsgiPy), 0o644); err != nil {
		t.Fatalf("failed to write wsgi.py: %v", err)
	}
	touchFile(t, repo, "pyproject.toml")

	cfg, err := AutoDetectBuildConfig(repo, pythonAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.RunCommand != "gunicorn wsgi:application --bind 0.0.0.0:${PORT:-8000}" {
		t.Fatalf("expected gunicorn wsgi command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigPythonASGI(t *testing.T) {
	repo := t.TempDir()
	asgiPy := `application = object()
`
	if err := os.WriteFile(filepath.Join(repo, "asgi.py"), []byte(asgiPy), 0o644); err != nil {
		t.Fatalf("failed to write asgi.py: %v", err)
	}
	touchFile(t, repo, "pyproject.toml")

	cfg, err := AutoDetectBuildConfig(repo, pythonAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.RunCommand != "uvicorn asgi:application --host 0.0.0.0 --port ${PORT:-8000}" {
		t.Fatalf("expected uvicorn asgi command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigGoRootMain(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "go.mod")
	mainGo := `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	cfg, err := AutoDetectBuildConfig(repo, goAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.Runtime != "go" {
		t.Fatalf("expected runtime go, got %q", cfg.Runtime)
	}
	if cfg.PrebuildCommand != "go mod download" {
		t.Fatalf("expected go mod download prebuild command, got %q", cfg.PrebuildCommand)
	}
	if cfg.BuildCommand != "go build -o app ." {
		t.Fatalf("expected go build root command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "./app" {
		t.Fatalf("expected go binary run command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigGoCmdEntrypoint(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "go.mod")
	if err := os.MkdirAll(filepath.Join(repo, "cmd", "api"), 0o755); err != nil {
		t.Fatalf("failed to create cmd/api: %v", err)
	}
	mainGo := `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(repo, "cmd", "api", "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("failed to write cmd/api/main.go: %v", err)
	}

	cfg, err := AutoDetectBuildConfig(repo, goAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.BuildCommand != "go build -o app ./cmd/api" {
		t.Fatalf("expected go build cmd command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "./app" {
		t.Fatalf("expected go binary run command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigGoTopLevelEntrypoint(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "go.mod")
	if err := os.MkdirAll(filepath.Join(repo, "server"), 0o755); err != nil {
		t.Fatalf("failed to create server dir: %v", err)
	}
	mainGo := `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(repo, "server", "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("failed to write server/main.go: %v", err)
	}

	cfg, err := AutoDetectBuildConfig(repo, goAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.BuildCommand != "go build -o app ./server" {
		t.Fatalf("expected go build top-level command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "./app" {
		t.Fatalf("expected go binary run command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigGoNestedEntrypoint(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "go.mod")
	if err := os.MkdirAll(filepath.Join(repo, "services", "api"), 0o755); err != nil {
		t.Fatalf("failed to create services/api dir: %v", err)
	}
	mainGo := `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(repo, "services", "api", "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("failed to write services/api/main.go: %v", err)
	}

	cfg, err := AutoDetectBuildConfig(repo, goAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.BuildCommand != "go build -o app ./services/api" {
		t.Fatalf("expected go build nested command, got %q", cfg.BuildCommand)
	}
	if cfg.RunCommand != "./app" {
		t.Fatalf("expected go binary run command, got %q", cfg.RunCommand)
	}
}

func TestAutoDetectBuildConfigGoWorkSyncPreferred(t *testing.T) {
	repo := t.TempDir()
	touchFile(t, repo, "go.mod")
	touchFile(t, repo, "go.work")
	mainGo := `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	cfg, err := AutoDetectBuildConfig(repo, goAllowedCommands())
	if err != nil {
		t.Fatalf("AutoDetectBuildConfig returned error: %v", err)
	}

	if cfg.PrebuildCommand != "go work sync" {
		t.Fatalf("expected go work sync prebuild command, got %q", cfg.PrebuildCommand)
	}
}
