# hubfly-builder

Standalone Go Builder for Hubfly.

## Endpoints

### Health Check

- **Endpoint:** `GET /healthz`
- **Description:** Returns a 200 OK status if the service is running.
- **Example:**
  ```bash
  curl -X GET http://localhost:8080/healthz
  ```

### Create a build job

- **Endpoint:** `POST /api/v1/jobs`
- **Description:** Creates a new build job.
- **Example:**
  ```bash
  curl -X POST http://localhost:8080/api/v1/jobs -H "Content-Type: application/json" -d '{
    "id": "build_1231q211",
    "projectId": "proj_1",
    "userId": "user_1",
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
      "prebuildCommand": "",
      "buildCommand": "",
      "runCommand": "npm start",
      "timeoutSeconds": 1800,
      "resourceLimits": {
        "cpu": 1,
        "memoryMB": 1024
      }
    }
  }'
  ```

### Get job status

- **Endpoint:** `GET /api/v1/jobs/{id}`
- **Description:** Retrieves the status of a specific build job.
- **Example:**
  ```bash
  curl -X GET http://localhost:8080/api/v1/jobs/build_1231q211
  ```
