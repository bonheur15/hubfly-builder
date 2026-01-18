package autodetect

import "fmt"

// GenerateDockerfile creates a Dockerfile content based on the runtime and version.
func GenerateDockerfile(runtime, version, prebuildCommand, buildCommand, runCommand string) ([]byte, error) {
	switch runtime {
	case "node":
		return generateNodeDockerfile(version, prebuildCommand, buildCommand, runCommand), nil
	case "python":
		return generatePythonDockerfile(version, prebuildCommand, buildCommand, runCommand), nil
	case "go":
		return generateGoDockerfile(version, prebuildCommand, buildCommand, runCommand), nil
	case "bun":
		return generateBunDockerfile(version, prebuildCommand, buildCommand, runCommand), nil
	case "static":
		return generateStaticDockerfile(), nil
	default:
		return nil, fmt.Errorf("unsupported runtime: %s", runtime)
	}
}

func generateStaticDockerfile() []byte {
	return []byte(`
FROM nginx:alpine

WORKDIR /usr/share/nginx/html

COPY . .

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
`)
}

func generateBunDockerfile(version, prebuildCommand, buildCommand, runCommand string) []byte {
	return []byte(fmt.Sprintf(`
FROM oven/bun:%s

WORKDIR /app

COPY . .

RUN %s
RUN %s

EXPOSE 3000

CMD %s
`, version, prebuildCommand, buildCommand, runCommand))
}

func generateNodeDockerfile(version, prebuildCommand, buildCommand, runCommand string) []byte {
	return []byte(fmt.Sprintf(`
FROM node:%s-alpine

WORKDIR /app

COPY . .

RUN %s
RUN %s

EXPOSE 3000

CMD %s
`, version, prebuildCommand, buildCommand, runCommand))
}

func generatePythonDockerfile(version, prebuildCommand, buildCommand, runCommand string) []byte {
	return []byte(fmt.Sprintf(`
FROM python:%s-slim

WORKDIR /app

COPY . .

RUN %s
RUN %s

EXPOSE 8000

CMD %s
`, version, prebuildCommand, buildCommand, runCommand))
}

func generateGoDockerfile(version, prebuildCommand, buildCommand, runCommand string) []byte {
	return []byte(fmt.Sprintf(`
FROM golang:%s-alpine

WORKDIR /app

COPY . .

RUN %s
RUN %s

EXPOSE 8080

CMD %s
`, version, prebuildCommand, buildCommand, runCommand))
}
