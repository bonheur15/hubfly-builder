# Hubfly Builder

**Hubfly Builder** Is a high-performance, standalone Go service designed to orchestrate container image builds through the Hubcell native build CLI. It provides a robust API for managing build jobs, supports automatic runtime detection, uses a built-in command allowlist for generated commands, and ensures persistence through a local SQLite database.

## Architecture & Features

- **Built with Go:** High-performance, concurrent execution model.
- **Hubcell Native Build Backend:** Runs `sudo hubcell build` directly for image builds.
- **SQLite Persistence:** All job metadata, status, and history are stored locally, allowing the builder to resume operations after restarts.
- **Auto-Detection (Zero-Config):** Automatically detects the runtime (Node.js, Bun, Go, Python, Java, etc.) and generates an optimized Dockerfile if one isn't provided.
- **Secure by Design:** Auto-detected commands are validated against a built-in allowlist.
- **Structured Logging:** Job logs are captured, stored locally, and served via API.
- **Backend Integration:** Reports build outcomes (success/failure) via configurable webhooks.
- **Hubcell Local Images:** Generated image tags use the `hubcell.local` registry expected by Hubcell builds.
- **Resource Management:** Supports configurable per-job resource limits (CPU/Memory).
- **Cleanup Automation:** Automatically prunes build workspaces and implements log retention policies.

---

## Configuration

### Environment Variables & Optional `configs/env.json`

The builder can be configured via environment variables or an optional JSON override file at `configs/env.json`. If the file is missing, the builder uses built-in defaults and does not generate the file.

| Key | Description | Default / Example |
| :--- | :--- | :--- |
| `HUBCELL_BASE_URL` | Hubcell API base URL used by ancillary Hubcell integrations | `http://127.0.0.1:10012` |
| `HUBCELL_CLI_PATH` | Hubcell executable path, or a directory containing `hubcell` | `/home/destroyer/Desktop/hubfly-cloud/hubcell/hubcell` |
| `CALLBACK_URL` | Backend webhook for reporting results | `https://hubfly.space/api/builds/callback` |

Example optional `configs/env.json`:

```json
{
  "HUBCELL_BASE_URL": "http://127.0.0.1:10012",
  "HUBCELL_CLI_PATH": "/home/destroyer/Desktop/hubfly-cloud/hubcell",
  "CALLBACK_URL": "https://hubfly.space/api/builds/callback"
}
```

### Hubcell Build Configuration

Build jobs call `sudo <HUBCELL_CLI_PATH> build` with the job image tag, requested network, memory bytes, CPU period/quota, and rootfs sizing flags.

---

## Runtime Layout

At runtime the builder creates and uses these local paths:

| Path | Purpose |
| :--- | :--- |
| `./data/hubfly-builder.sqlite` | SQLite database for jobs and state |
| `./log/` | System log and per-job build logs |
| `./configs/env.json` | Optional local config overrides |

Make sure the process user can create and write these paths.

---

## Supported Runtimes & Auto-Detection

When `isAutoBuild` is set to `true`, the builder inspects the repository root (or the specified `workingDir`) to identify the runtime:

| Runtime | Detection File | Default Image |
| :--- | :--- | :--- |
| **Bun** | `bun.lock` | `oven/bun:1.2` |
| **Node.js** | `package.json` | `node:18-alpine` |
| **Go** | `go.mod` | `golang:1.18-alpine` |
| **Python** | `requirements.txt`, `pyproject.toml`, `setup.py`, `Pipfile` | `python:3.14.4-slim` |
| **Java** | `pom.xml`, `build.gradle`, `build.gradle.kts` | `maven:3.9-eclipse-temurin-17` / `gradle:8-jdk17` |
| **Static** | `index.html` | `nginx:alpine` |
| **PHP** | `composer.json` | `php:8.3-apache` / `php:8.3-fpm` / `php:8.3-cli` |

If a `Dockerfile` exists in the context, it takes precedence over auto-detection.

---

## Image Tagging Scheme

Images are tagged according to the following pattern:
`hubcell.local/{USER_ID}/{PROJECT_ID}:{SHORT_COMMIT_SHA}-b{BUILD_ID}-v{TIMESTAMP}`

**Example:**
`hubcell.local/user-123/my-app:abc123456789-b-build-456-v20260210T123000Z`

---

## API Documentation

### 1. Create Build Job
Creates a new build job and queues it for execution.

- **URL:** `/api/v1/jobs`
- **Method:** `POST`
- **Payload:**

```json
{
  "id": "build_uuid_123",
  "projectId": "my-awesome-project",
  "userId": "user_99",
  "sourceType": "git",
  "sourceInfo": {
    "gitRepository": "https://github.com/user/repo.git",
    "ref": "main",
    "commitSha": "optional_full_sha",
    "workingDir": "src"
  },
  "buildConfig": {
    "isAutoBuild": true,
    "runtime": "bun",
    "version": "1.2",
    "prebuildCommand": "bun install",
    "buildCommand": "bun run build",
    "dockerfileArgs": {
      "BUILD_VERSION": "2026.05.01"
    },
    "dockerfileEnv": {
      "APP_ENV": "production"
    },
    "network": "user123_net",
    "env": {
      "NEXT_PUBLIC_API_URL": "https://api.example.com",
      "DATABASE_URL": "postgres://...",
      "SENTRY_AUTH_TOKEN": "..."
    },
    "envOverrides": {
      "NEXT_PUBLIC_API_URL": { "secret": true },
      "DATABASE_URL": { "scope": "build", "secret": true }
    },
    "timeoutSeconds": 3600,
    "resourceLimits": {
      "cpu": 0.5,
      "memoryMB": 2048
    }
  }
}
```

`buildConfig.env` is always treated in `auto` mode:
- Public-prefixed vars (e.g. `NEXT_PUBLIC_`, `VITE_`) are resolved as `both` (build + runtime).
- Keys with build evidence (`Dockerfile ARG`/reference or known build config references) are resolved to `build`.
- Unknown keys default to `runtime`.
- Unknown/sensitive keys default to `secret`; native Hubcell builds currently log a warning because the CLI does not accept secret mounts.
- The resolved result is returned as `buildConfig.resolvedEnvPlan` and callback metadata (`runtimeEnvKeys`).

`buildConfig.envOverrides` is optional:
- If provided for a key, override values take precedence over auto-detection.
- `scope` supports `build`, `runtime`, or `both`.
- `secret` (`true`/`false`) forces whether the key is mounted as a build secret vs passed as build-arg when build scope is active.

`buildConfig.dockerfileArgs` and `buildConfig.dockerfileEnv` are optional and only apply when a `Dockerfile` is found in the repository:
- `dockerfileArgs` are injected as Dockerfile `ARG` declarations.
- `dockerfileEnv` entries are injected as `ARG` + `ENV` declarations.
- These fields are ignored for generated Dockerfiles and for `customDockerfile`.
- Do not put secrets in `dockerfileEnv`; `ENV` values are baked into the resulting image.

`buildConfig.buildContextDir` is optional for repository Dockerfiles:
- By default, the Dockerfile build context is the repository root (`"."`), even when `sourceInfo.workingDir` points to a subdirectory Dockerfile.
- Set it to a narrower ancestor directory when you want a smaller context.
- The context must stay inside the repository and must contain `sourceInfo.workingDir`.
- Use `.dockerignore` to keep a wider context isolated to only the files the Dockerfile needs.

`buildConfig.customDockerfile` is optional:
- Send plain Dockerfile text in this field to force the builder to use that Dockerfile.
- A custom Dockerfile takes precedence over any `Dockerfile` committed in the repository.
- The build context defaults to `sourceInfo.workingDir` when a custom Dockerfile is provided.
- Example: `"customDockerfile": "FROM node:22-alpine\nWORKDIR /app\nCOPY . .\nRUN npm ci\nCMD [\"npm\", \"start\"]\n"`

`buildConfig.network` is required:
- The worker passes this value to `hubcell build --network`.
- Build requests do not add Linux capabilities.
- If missing/empty, the job is rejected with `no user network provided`.

### Gateway Port Mapping

- This applies to static sites served by the generated nginx runtime.
- Static nginx listens on port `80` and `8080` by default, and both are exposed in the generated Dockerfile.
- Callback payload includes `exposePort` for static runtime only.

Examples:
- Docker publish: `-p 80:8080`
- Kubernetes Service: `port: 80`, `targetPort: 8080`
- Nginx reverse proxy: `proxy_pass http://app:8080;`

Callback payload excerpt:
```json
{
  "id": "build_uuid_123",
  "status": "success",
  "imageTag": "hubcell.local/user-123/my-app:abc123-bbuild_uuid_123-v20260210T123000Z",
  "exposePort": "8080"
}
```

- **Responses:**
  - `201 Created`: Job successfully queued. The response body includes the fully populated `BuildConfig`, including the auto-generated `dockerfileContent` (if `isAutoBuild` was `true`).
  - `400 Bad Request`: Invalid payload or failed repository inspection.
  - `500 Internal Server Error`: Storage failure.

- **Example:**
```bash
curl -X POST http://localhost:10008/api/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{"id":"b1", "projectId":"p1", "userId":"u1", "sourceType":"git", "sourceInfo":{"gitRepository":"https://github.com/bonheur15/hubfly-sample-react-bun.git"}, "buildConfig":{"isAutoBuild":true,"network":"proj-network-p1"}}'
```

### 2. Get Job Status
Retrieves the full metadata and current status of a job.

- **URL:** `/api/v1/jobs/{id}`
- **Method:** `GET`
- **Responses:**
  - `200 OK`: Returns the `BuildJob` object.
  - `404 Not Found`: `{"error": "JOB_NOT_FOUND", "message": "job not found"}`

- **Example:**
```bash
curl -i http://localhost:10008/api/v1/jobs/b1
```

### 3. Get Job Logs
Returns the raw text logs of the build process.

- **URL:** `/api/v1/jobs/{id}/logs`
- **Method:** `GET`
- **Responses:**
  - `200 OK`: `text/plain` stream of logs.
  - `404 Not Found`: `{"error": "BUILD_LOG_NOT_FOUND", "message": "build log not found"}`

- **Example:**
```bash
curl http://localhost:10008/api/v1/jobs/b1/logs
```

### 4. Health Check
Basic availability check.

- **URL:** `/healthz`
- **Method:** `GET`
- **Response:** `200 OK` ("OK")

---

## Development & Debugging Endpoints

### List Running Builds
Lists all jobs currently in `claimed` or `building` state.

- **URL:** `/dev/running-builds`
- **Method:** `GET`

### Reset Database
Clears all jobs from the SQLite database. **Use with caution.**

- **URL:** `/dev/reset-db`
- **Method:** `POST`

---

## Errors and Status Codes

| Code | Status | Meaning |
| :--- | :--- | :--- |
| `pending` | 201 | Job created, waiting for worker. |
| `claimed` | - | Job picked up by a worker. |
| `building` | - | Hubcell build or Git operations in progress. |
| `success` | - | Build completed successfully. |
| `failed` | - | An error occurred during the build process. |
| `canceled` | - | Job was manually terminated. |

---

## Getting Started

### Linux Prerequisites

The builder is intended to run on Linux with Hubcell available locally. Hubcell performs native Dockerfile builds and stores the resulting image under `hubcell.local`.

Required commands:

- `sudo`
- the Hubcell CLI referenced by `HUBCELL_CLI_PATH`
- `git`

To build the binary from source, you also need a working Go toolchain installed locally.

Recommended baseline packages on Debian/Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y git curl ca-certificates
```

Before starting the builder, verify the host is ready:

```bash
curl -fsS "$HUBCELL_BASE_URL/v1/cells" >/dev/null
sudo "$HUBCELL_CLI_PATH" build --help
git --version
```

The builder process must be able to:

- talk to the Hubcell API
- run the Hubcell CLI through `sudo`
- clone Git repositories over the network
- write to `./data` and `./log`

### Builder Runtime Notes

For each job, the builder:

- clones the repository into a temporary workspace
- generates or stages the Dockerfile when needed
- generates a `hubcell.local/<user>/<project>:<source>-b<job>-v<timestamp>` image tag
- runs `sudo <HUBCELL_CLI_PATH> build -t <generated-image-tag> --network <request-buildConfig.network> -m <bytes> --cpu-period <period> --cpu-quota <quota> --rootfs-initial-size 10g <dockerfile-directory>`
- records the resulting image tag
- removes the temporary workspace

The Hubcell virtual network named by `buildConfig.network` is passed directly to the Hubcell build CLI.

### Build From Source
```bash
git clone https://github.com/hubfly/hubfly-builder.git
cd hubfly-builder
go mod download
go build -o hubfly-builder ./cmd/hubfly-builder
```

### Release Bundles

The GitHub release publishes per-platform bundles:

- `hubfly-builder_linux_amd64.tar.gz`
- `hubfly-builder_linux_arm64.tar.gz`
- `hubfly-builder_darwin_amd64.tar.gz`
- `hubfly-builder_darwin_arm64.tar.gz`
- `hubfly-builder_windows_amd64.zip`

Each release asset also has a matching `.sha256` checksum file. Extracting a bundle places the `hubfly-builder` binary (or `hubfly-builder.exe` on Windows) at the archive root, alongside `README.md` and the `configs/` directory.

### Run The Server
```bash
./hubfly-builder
```

The server will start on port `10008` by default.
Run it from the project root or the extracted release bundle root so the relative `./configs`, `./data`, and `./log` paths resolve correctly.

### Development Run

```bash
go run ./cmd/hubfly-builder
```

### Version Output

Release builds inject the version from the Git tag. To print it:

```bash
./hubfly-builder version
```

This command prints only the version string.

### First-Run Checklist

- ensure Hubcell is running and reachable through `HUBCELL_BASE_URL`
- ensure `HUBCELL_CLI_PATH` points to the Hubcell CLI or its containing directory
- ensure the builder user can run the Hubcell CLI through `sudo`
- ensure `CALLBACK_URL` is reachable from the builder host
- ensure the process user can write `./data` and `./log`

---

## Utility Commands

### Manual Build Test

To test a build manually using the configured Hubcell CLI:

```bash
sudo "$HUBCELL_CLI_PATH" build \
  -t hubcell.local/test-image:latest \
  --network project-network-demo \
  -m 4294967296 \
  --cpu-period 100000 \
  --cpu-quota 200000 \
  --rootfs-initial-size 10g \
  .
```
