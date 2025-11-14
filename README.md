# hubfly-builder

Standalone Go Builder for Hubfly.

This service receives build jobs, executes them using BuildKit, streams logs, pushes images to a registry, and reports results back to a backend. It uses a local SQLite database for persistence, allowing it to resume jobs after a restart.

## Features

- **Concurrent Builds:** Manages a configurable number of concurrent build jobs.
- **Persistent Job Queue:** Uses SQLite to persist the job queue, allowing for recovery after restarts.
- **Git Integration:** Clones Git repositories to use as a build context.
- **BuildKit Integration:** Uses `buildctl` to build Dockerfiles and push images to a registry.
- **Command Allowlist:** Restricts executable commands to a safe list defined in `configs/allowed-commands.json`.
- **Backend Reporting:** Reports build status (success or failure) back to a configurable webhook URL.
- **Automatic Cleanup:**
  - Workspaces are automatically cleaned up after each build.
  - Old log files are periodically deleted based on a retention policy.
- **Retry Logic:** Automatically retries failed builds up to a configurable number of times.

## Getting Started

### Prerequisites

- Go 1.18+
- BuildKit (`buildkitd` running and `buildctl` in the system's PATH)
- A running container registry (if pushing images)

### Running the Builder

1.  **Start BuildKit (if not already running):**
    ```bash
    # Example using Docker
    docker run -d --name buildkitd --privileged moby/buildkit:latest
    export BUILDKIT_ADDR=docker-container://buildkitd
    ```

2.  **Run the builder service:**
    The service can be configured via environment variables.

    ```bash
    # Example:
    export REGISTRY_URL="localhost:5000"
    export CALLBACK_URL="http://localhost:3000/api/builds/callback" # Your backend endpoint

    go run ./cmd/hubfly-builder
    ```

## Endpoints

All endpoints are served on port `:8080`.

### Health Check

- **Endpoint:** `GET /healthz`
- **Description:** Returns a 200 OK status if the service is running.
- **Example:**
  ```bash
  curl -i -X GET http://localhost:8080/healthz
  ```

### Create a Build Job

- **Endpoint:** `POST /api/v1/jobs`
- **Description:** Creates a new build job. The job is added to the queue and will be picked up by the executor.
- **Example:**
  ```bash
  curl -X POST http://localhost:8080/api/v1/jobs -H "Content-Type: application/json" -d '{
    "id": "build_1",
    "projectId": "my-project",
    "userId": "user_123",
    "sourceType": "git",
    "sourceInfo": {
      "gitRepository": "https://github.com/bonheur15/hubfly-sample-react-bun.git",
      "commitSha": "abcdef",
      "ref": "main"
    },
    "buildConfig": {
      "isAutoBuild": true,
      "runtime": "node",
      "version": "18",
      "prebuildCommand": "npm install",
      "buildCommand": "npm run build",
      "runCommand": "npm start",
      "timeoutSeconds": 1800,
      "resourceLimits": {
        "cpu": 1,
        "memoryMB": 1024
      }
    }
  }'
  ```

### Get Job Status

- **Endpoint:** `GET /api/v1/jobs/{id}`
- **Description:** Retrieves the status and details of a specific build job.
- **Example:**
  ```bash
  curl -X GET http://localhost:8080/api/v1/jobs/build_1
  ```

### Get Job Logs

- **Endpoint:** `GET /api/v1/jobs/{id}/logs`
- **Description:** Retrieves the logs for a specific build job.
- **Example:**
  ```bash
  curl -X GET http://localhost:8080/api/v1/jobs/build_1/logs
  ```

### List Running Builds (Dev Endpoint)

- **Endpoint:** `GET /dev/running-builds`
- **Description:** A development-only endpoint that returns a list of jobs currently in the 'claimed' or 'building' state.
- **Example:**
  ```bash
  curl -X GET http://localhost:8080/dev/running-builds
  ```