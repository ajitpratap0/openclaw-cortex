# Deployment Guide

## Local Development

### Quick Start

```bash
# Prerequisites: Go 1.25+, Docker, Ollama, Task
task docker:up              # Start Qdrant
ollama pull nomic-embed-text # Pull embedding model
task build                   # Build binary
```

### Running

```bash
export ANTHROPIC_API_KEY=sk-ant-...

# Index memory files
openclaw-cortex index --path ~/.openclaw/workspace/memory/

# Search
openclaw-cortex search "deployment best practices"

# Interactive usage
openclaw-cortex recall "How do I handle errors?" --budget 2000
```

## Docker

### Build & Run

```bash
task docker:build
docker run --rm \
  -e ANTHROPIC_API_KEY=sk-ant-... \
  -e OPENCLAW_CORTEX_QDRANT_HOST=host.docker.internal \
  openclaw-cortex:latest search "query"
```

### Docker Compose (Full Stack)

```bash
task docker:up   # Starts Qdrant with persistent volume
task docker:down # Stops Qdrant
```

Qdrant exposes:
- HTTP API: `localhost:6333`
- gRPC API: `localhost:6334`

Data persists in the `qdrant_data` Docker volume.

## Kubernetes

### Qdrant StatefulSet

```bash
kubectl apply -f k8s/qdrant.yaml
```

This creates:
- Namespace `cortex`
- StatefulSet with 1 replica
- PVC for persistent storage
- ClusterIP services for HTTP (6333) and gRPC (6334)

### Cortex as a Sidecar/CronJob

For periodic indexing and lifecycle management:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: cortex-consolidate
  namespace: openclaw-cortex
spec:
  schedule: "0 */6 * * *"  # Every 6 hours
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: cortex
            image: openclaw-cortex:latest
            args: ["consolidate"]
            env:
            - name: OPENCLAW_CORTEX_QDRANT_HOST
              value: qdrant.cortex.svc.cluster.local
            - name: ANTHROPIC_API_KEY
              valueFrom:
                secretKeyRef:
                  name: cortex-secrets
                  key: anthropic-api-key
          restartPolicy: OnFailure
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | — | Required for capture |
| `OPENCLAW_CORTEX_QDRANT_HOST` | `localhost` | Qdrant hostname |
| `OPENCLAW_CORTEX_QDRANT_GRPC_PORT` | `6334` | Qdrant gRPC port |
| `OPENCLAW_CORTEX_QDRANT_HTTP_PORT` | `6333` | Qdrant HTTP port |
| `OPENCLAW_CORTEX_OLLAMA_BASE_URL` | `http://localhost:11434` | Ollama endpoint |
| `OPENCLAW_CORTEX_MEMORY_DIR` | `~/.openclaw/workspace/memory/` | Memory files path |

### Config File

Place at `~/.openclaw-cortex/config.yaml`. See [README](../README.md#configuration) for full schema.

## Health Checks

```bash
# Verify Qdrant is reachable
curl http://localhost:6333/healthz

# Verify Ollama model is available
ollama list | grep nomic-embed-text

# Verify openclaw-cortex connectivity
openclaw-cortex stats
```

## Backup & Restore

### Qdrant Snapshots

```bash
# Create snapshot
curl -X POST http://localhost:6333/collections/cortex_memories/snapshots

# List snapshots
curl http://localhost:6333/collections/cortex_memories/snapshots

# Restore: download snapshot and use Qdrant restore API
```

### Memory Files
Memory files are plain markdown — back up via git or filesystem copy:
```bash
cd ~/.openclaw/workspace/memory && git add -A && git commit -m "backup"
```
