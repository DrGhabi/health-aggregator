# Health Aggregator

Health Aggregator is a minimalistic tool that monitors the health of resources (Pods, Deployments, ReplicaSets, and Jobs) 
in a specified Kubernetes namespace. It can compare the actual state of resources against an expected configuration.

## Features

- Monitors Pods, Deployments, ReplicaSets, and Jobs.
- Supports expected state configuration via YAML with `min` and `max` constraints.
- Provides status indicators: `[OK]`, `[MISSING]`, `[MISMATCH]`, and `[OVER LIMIT]`.
- Configurable periodic console reporting and web API.
- Optional debug logging for endpoint access.
- Two web endpoints (public and health):
    - **Main Endpoint**: (public) Returns aggregated JSON health status (`up`, `down`, `warning`).
    - **Health Endpoint**: (health) Standard health check of this tool itself for Kubernetes liveness/readiness.
    - **Metrics Endpoint**: (health) Optional Prometheus metrics endpoint (default path: `/metrics`).
- Optional SSL/TLS support for web endpoints.
- Configurable global log level (`debug`, `info`, `warn`, `error`).

## Configuration

You can provide a configuration file to describe the expected resources and server settings.

### Configuration Format (YAML)

```yaml
server:
  main:
    port: 8080
    # Optional TLS configuration
    # tls:
    #   certFile: "/path/to/cert.pem"
    #   keyFile: "/path/to/key.pem"
  health:
    port: 8081
  metrics:
    enabled: true
    path: "/metrics" # Optional, defaults to /metrics
logLevel: "info" # Optional, sets global logging level (debug, info, warn, error)

resources:
  pods:
    - name: "my-app-pod"
      min: 1 # Minimum number of pods required for "up" state
      max: 5 # Optional: Maximum number of pods. If exceeded, state is "warning".
  deployments:
    - name: "my-deployment"
      replicas: 3 # Expected number of available replicas
      max: 10     # Optional maximum
  replicaSets:
    - name: "my-replicaset"
      replicas: 1
  jobs:
    - name: "my-cleanup-job"
```

### Health States & HTTP Status Codes (Main Endpoint)

- **`{ "health": "up" }`**: All resources meet expected minimums. (HTTP 200)
- **`{ "health": "down" }`**: At least one resource is below `min` (or `replicas`). (HTTP 503)
- **`{ "health": "warning" }`**: All resources meet minimums, but at least one exceeds `max`. (HTTP 509)

### Example Configuration

```yaml
resources:
  pods:
    - name: "frontend-pod"
  deployments:
    - name: "api-server"
      replicas: 2
  jobs:
    - name: "db-migration"
```

## Metrics

When enabled, the application provides Prometheus metrics at the configured endpoint (default `/metrics` on the health port).

### Available Metrics

| Metric Name | Type | Labels | Description |
| ----------- | ---- | ------ | ----------- |
| `aggregup_resources_total` | Gauge | `namespace`, `resource_type` | Total number of monitored resources. |
| `aggregup_resources_healthy` | Gauge | `namespace`, `resource_type` | Number of healthy monitored resources. |
| `aggregup_namespace_health` | Gauge | `namespace` | Overall health of the namespace (1=up, 0.5=warning, 0=down). |

### Metric Values for Namespace Health
- `1.0`: Healthy (`up`)
- `0.5`: Warning (`warning`)
- `0.0`: Unhealthy (`down`)

### Logging

The application supports the following log levels:

| Level | Description |
| ----- | ----------- |
| `debug` | Detailed information, including endpoint access logs. |
| `info`  | General operational information (startup, periodic reports). **Default level.** |
| `warn`  | Warning messages (e.g., resource limit exceeded). |
| `error` | Error messages (e.g., Kubernetes API failures). |

Log messages are prefixed with their level, for example: `[INFO] Starting health aggregator...`.

## Usage

### Environment Variables

- `NAMESPACE`: The Kubernetes namespace to monitor (default: current namespace).
- `CONFIG_PATH`: Path to the YAML configuration file.
- `LOG_LEVEL`: Global logging level (`debug`, `info`, `warn`, `error`).
- `DEBUG`: Set to `true` to enable debug logging (legacy, use `LOG_LEVEL=debug` instead).
- `KUBECONFIG`: Path to your Kubernetes config file (optional, defaults to `~/.kube/config`).
- `SERVICE_ACCOUNT_NAME`: Service account to impersonate (optional).

### Command Line Flags

- `--config`: Path to the YAML configuration file (overrides `CONFIG_PATH`).

### Running the Application

```powershell
./health-aggregator.exe --config config.yaml
```

## Development

### Prerequisites

- Go 1.26+
- Task (optional, for running predefined tasks)

### Building

```powershell
task build
```

### Testing

```powershell
task test
```

### Coverage

To run tests and see the coverage report:

```powershell
task coverage
```
