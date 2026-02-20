package autodetect

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateDockerfile creates Dockerfile content based on the runtime and version.
func GenerateDockerfile(runtime, version, prebuildCommand, buildCommand, runCommand string) ([]byte, error) {
	return GenerateDockerfileWithBuildEnv(runtime, version, prebuildCommand, buildCommand, runCommand, nil, nil)
}

// GenerateDockerfileWithBuildEnv creates Dockerfile content and wires build-time env support.
func GenerateDockerfileWithBuildEnv(runtime, version, prebuildCommand, buildCommand, runCommand string, buildArgKeys, secretBuildKeys []string) ([]byte, error) {
	switch runtime {
	case "node":
		return generateAppDockerfile("node:"+version+"-alpine", "/app", "3000", prebuildCommand, buildCommand, runCommand, buildArgKeys, secretBuildKeys), nil
	case "python":
		return generateAppDockerfile("python:"+version+"-slim", "/app", "8000", prebuildCommand, buildCommand, runCommand, buildArgKeys, secretBuildKeys), nil
	case "go":
		return generateAppDockerfile("golang:"+version+"-alpine", "/app", "8080", prebuildCommand, buildCommand, runCommand, buildArgKeys, secretBuildKeys), nil
	case "bun":
		return generateAppDockerfile("oven/bun:"+version, "/app", "3000", prebuildCommand, buildCommand, runCommand, buildArgKeys, secretBuildKeys), nil
	case "java":
		return generateAppDockerfile(selectJavaBaseImage(version, prebuildCommand, buildCommand), "/app", "8080", prebuildCommand, buildCommand, runCommand, buildArgKeys, secretBuildKeys), nil
	case "static":
		return generateStaticDockerfile(), nil
	default:
		return nil, fmt.Errorf("unsupported runtime: %s", runtime)
	}
}

func selectJavaBaseImage(version, prebuildCommand, buildCommand string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "17"
	}

	combined := strings.ToLower(strings.TrimSpace(prebuildCommand + " " + buildCommand))
	switch {
	case strings.Contains(combined, "gradle"), strings.Contains(combined, "./gradlew"):
		return "gradle:8-jdk" + version
	case strings.Contains(combined, "mvn"), strings.Contains(combined, "./mvnw"):
		return "maven:3.9-eclipse-temurin-" + version
	default:
		return "eclipse-temurin:" + version + "-jdk"
	}
}

func generateStaticDockerfile() []byte {
	return []byte(`FROM nginx:alpine

WORKDIR /usr/share/nginx/html

COPY . .

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
`)
}

func generateAppDockerfile(baseImage, workDir, exposePort, prebuildCommand, buildCommand, runCommand string, buildArgKeys, secretBuildKeys []string) []byte {
	buildArgKeys = normalizeKeys(buildArgKeys)
	secretBuildKeys = normalizeKeys(secretBuildKeys)

	var builder strings.Builder
	fmt.Fprintf(&builder, "FROM %s\n\n", baseImage)
	fmt.Fprintf(&builder, "WORKDIR %s\n\n", workDir)
	builder.WriteString("COPY . .\n\n")

	if argLines := renderArgLines(buildArgKeys); argLines != "" {
		builder.WriteString(argLines)
	}
	if runLine := renderRunLine(prebuildCommand, secretBuildKeys); runLine != "" {
		builder.WriteString(runLine)
	}
	if runLine := renderRunLine(buildCommand, secretBuildKeys); runLine != "" {
		builder.WriteString(runLine)
	}

	fmt.Fprintf(&builder, "\nEXPOSE %s\n\n", exposePort)
	if cmdLine := renderCmdLine(runCommand); cmdLine != "" {
		builder.WriteString(cmdLine)
	}

	return []byte(strings.TrimSpace(builder.String()) + "\n")
}

func renderArgLines(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&builder, "ARG %s\n", key)
	}
	builder.WriteString("\n")
	return builder.String()
}

func renderRunLine(command string, secretBuildKeys []string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	if len(secretBuildKeys) == 0 {
		return fmt.Sprintf("RUN %s\n", command)
	}

	mountFlags := make([]string, 0, len(secretBuildKeys))
	exports := make([]string, 0, len(secretBuildKeys))
	for _, key := range secretBuildKeys {
		mountFlags = append(mountFlags, fmt.Sprintf("--mount=type=secret,id=%s", key))
		exports = append(exports, fmt.Sprintf("export %s=\"$(cat /run/secrets/%s)\";", key, key))
	}

	payload := "set -e; " + strings.Join(exports, " ") + " " + command
	return fmt.Sprintf("RUN %s sh -c '%s'\n", strings.Join(mountFlags, " "), escapeSingleQuotes(payload))
}

func renderCmdLine(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	return fmt.Sprintf("CMD %s\n", command)
}

func escapeSingleQuotes(value string) string {
	return strings.ReplaceAll(value, "'", "'\"'\"'")
}

func normalizeKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}

	sort.Strings(out)
	return out
}
