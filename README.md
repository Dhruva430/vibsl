# vibsl — Queue-Based Docker Build & Kubernetes Deploy API

A small Go service that accepts **deployment jobs** over HTTP, queues them, and
runs each through a pipeline that:

1. **Clones** a Git repository (pure-Go, shallow)
2. **Builds** a Docker image from its `Dockerfile`
3. **Pushes** the image to a local/test registry
4. **Generates** a full set of Kubernetes manifests from the job spec
5. **Applies** them to a cluster using the Kubernetes Go client (`client-go`)
   via **server-side apply**

Job status is tracked in-memory and exposed through a REST API.

> **Repo:** https://github.com/Dhruva430/vibsl
> **Sample workload:** https://github.com/Dhruva430/vibsl-sample-app

---

## Architecture

```
                POST /jobs
  client ───────────────────────►  API (net/http, Go 1.22 routing)
                                      │  validate + default + store (pending)
                                      ▼
                              in-memory Store ◄──── GET /jobs, /jobs/{id}
                                      │
                                      ▼  enqueue id
                              bounded Queue (chan)
                                      │
                          ┌───────────┴───────────┐  N workers
                          ▼                        ▼
                                  Pipeline.Run(id)
        ┌──────────┬──────────┬──────────┬────────────────────┐
        ▼          ▼          ▼          ▼                    ▼
     clone      build       push      generate            apply (SSA)
   (go-git)  (docker CLI) (docker)   (typed API objs)   (dynamic client)
                                          │
                                          ├─ RenderYAML  → inspect / sample
                                          └─ ApplyAll    → live cluster
```

Each package has a single responsibility:

| Package              | Responsibility                                             |
|----------------------|------------------------------------------------------------|
| `internal/config`    | Env-driven configuration                                   |
| `internal/job`       | Job spec, status model, validation/defaults, thread-safe store |
| `internal/queue`     | Bounded channel queue + worker pool                        |
| `internal/git`       | Shallow clone via `go-git`                                 |
| `internal/docker`    | Build/push via the `docker` CLI (BuildKit), behind an interface |
| `internal/k8s`       | Manifest generation (typed objects), YAML render, server-side apply |
| `internal/pipeline`  | Orchestrates clone → build → push → deploy, streams logs   |
| `internal/api`       | HTTP handlers + access logging                             |
| `cmd/server`         | Wires everything together, graceful shutdown               |
| `cmd/render`         | Renders manifests from a job JSON (no build/deploy)        |

---

## Requirements

- **Go 1.23+**
- **Docker** (running daemon; used for build + push)
- A **Kubernetes cluster** + a **container registry** the cluster can pull from.
  The included script provisions both with [kind](https://kind.sigs.k8s.io/).
- `kubectl` (optional, for inspection) and `kind` (for the local cluster).

---

## Quick start

```bash
# 1. Stand up a local cluster + registry on localhost:5001
#    (also installs ingress-nginx + metrics-server in the demo)
make cluster-up

# 2. Run the API (uses ~/.kube/config to reach the cluster)
make run
#   → listening on :8080

# 3. In another shell, submit the sample job
make submit            # POST examples/job.json
#   → {"id":"…","phase":"pending", …}

# 4. Watch it progress
curl -s localhost:8080/jobs | jq '.jobs[] | {id, phase, image_ref}'

# 5. Inspect what landed in the cluster
kubectl get all,ingress,hpa,pdb,networkpolicy,role,rolebinding -n demo
```

### Configuration (environment variables)

| Variable           | Default                 | Meaning                                        |
|--------------------|-------------------------|------------------------------------------------|
| `HTTP_ADDR`        | `:8080`                 | API listen address                             |
| `WORKERS`          | `2`                     | Concurrent pipeline workers                    |
| `QUEUE_SIZE`       | `64`                    | Queue buffer capacity                          |
| `WORK_DIR`         | `$TMPDIR/vibsl-jobs`    | Parent dir for per-job checkouts               |
| `KUBECONFIG`       | *(default loading)*     | Explicit kubeconfig path                       |
| `DEFAULT_REGISTRY` | `localhost:5001`        | Registry used when a job omits `image.registry`|
| `DRY_RUN_K8S`      | `false`                 | Render manifests but don't apply them          |
| `JOB_TIMEOUT_SECONDS` | `900`                | Per-job wall-clock timeout                      |
| `FIELD_MANAGER`    | `vibsl-controller`      | Server-side-apply field manager                |

Run **without a cluster** (still clones, builds, pushes, and renders manifests):

```bash
DRY_RUN_K8S=true make run
```

---

## API

### `POST /jobs`
Submit a deployment job. Returns `202 Accepted` with the stored job (id +
`pending` phase). Unknown JSON fields are rejected.

```bash
curl -X POST localhost:8080/jobs -H 'Content-Type: application/json' \
  --data @examples/job.json
```

### `GET /jobs/{job_id}`
Full job record: phase, message, `image_ref`, applied resources, and the
structured pipeline log. `404` if unknown.

### `GET /jobs`
All jobs, newest first: `{"jobs": [ … ]}`.

### `GET /healthz`
`{"status":"ok","queue_depth":N,"time":"…"}`.

### Job lifecycle

```
pending → cloning → building → pushing → deploying → succeeded
                                                    ↘ failed
```

---

## Sample job JSON

See [`examples/job.json`](examples/job.json):

```json
{
  "name": "demo-app",
  "repo_url": "https://github.com/Dhruva430/vibsl-sample-app.git",
  "git_ref": "main",
  "dockerfile": "Dockerfile",
  "build_context": ".",
  "image": { "registry": "localhost:5001", "name": "demo-app", "tag": "v1" },
  "namespace": "demo",
  "replicas": 2,
  "container_port": 8080,
  "service_port": 80,
  "host": "demo-app.local",
  "env": { "GREETING": "Hello from vibsl" },
  "secrets": { "API_KEY": "placeholder-rotate-me" },
  "autoscaling": { "enabled": true, "min_replicas": 2, "max_replicas": 5, "target_cpu_utilization": 70 }
}
```

Only `name` and `repo_url` are required; everything else is defaulted (see
[`internal/job/defaults.go`](internal/job/defaults.go)).

---

## Generated Kubernetes resources

For each job the service generates **12 resources** (in dependency order):

| # | Kind | Notes |
|---|------|-------|
| 1 | **Namespace** | target namespace |
| 2 | **ServiceAccount** | workload identity; token automount disabled |
| 3 | **ConfigMap** | `spec.env` → `<name>-config`, wired via `envFrom` |
| 4 | **Secret** (placeholder) | `spec.secrets` → `<name>-secret` (Opaque) |
| 5 | **Role** | least-privilege read on configmaps/secrets |
| 6 | **RoleBinding** | binds the Role to the ServiceAccount |
| 7 | **Deployment** | non-root, read-only rootfs, caps dropped, probes, resources |
| 8 | **Service** | ClusterIP, named `http` target port |
| 9 | **Ingress** | `networking.k8s.io/v1`, class `nginx`, host routing |
| 10 | **HorizontalPodAutoscaler** | `autoscaling/v2`, CPU target |
| 11 | **PodDisruptionBudget** | `minAvailable: 1` |
| 12 | **NetworkPolicy** | default-deny + ingress to app port + DNS/egress |

A fully rendered example is checked in at
[`examples/manifests/demo-app.yaml`](examples/manifests/demo-app.yaml). Generate
your own from any job spec without deploying:

```bash
go run ./cmd/render examples/job.json     # or: make render
```

Because manifests are authored as **typed `k8s.io/api` structs**, the exact same
objects feed both the YAML you can inspect and the server-side apply that hits
the cluster — one source of truth, no template drift.

---

## How it applies to the cluster

`internal/k8s/apply.go` uses `client-go`'s **dynamic client** plus a
discovery-backed `RESTMapper`. Each typed object is converted to
`unstructured`, its GVK resolved to a REST resource, and applied with
`types.ApplyPatchType` (server-side apply) under a named field manager. This is
generic: new resource kinds need no extra wiring, and re-submitting a job
reconciles existing objects instead of erroring.

---

## Development

```bash
make build     # build ./bin/server and ./bin/render
make test      # unit tests (manifest generation invariants)
make vet
```

See [`docs/DEMO.md`](docs/DEMO.md) for a captured end-to-end run (logs).

---

## Assumptions & limitations

**Assumptions**
- Docker daemon is reachable and can push to the configured registry.
- The cluster can pull from that registry. With kind, the included script wires
  `localhost:5001` into containerd on every node.
- The target repo has a buildable `Dockerfile` at `build_context/dockerfile`.
- The app serves HTTP `GET /` with a 2xx/3xx so the default probes pass.

**Limitations (by design, for a compact demo)**
- **State is in-memory.** Jobs and queue are lost on restart. The `job.Store`
  method set is the seam for a DB/Redis-backed implementation.
- **At-most-once processing.** No retries, dead-letter queue, or idempotency
  keys; a crashed worker drops its in-flight job.
- **Build uses the `docker` CLI** (BuildKit) via `exec`, not the Docker SDK —
  simplest reliable path. It is behind the `docker.Builder` interface so a
  rootless/buildkit/SDK builder can be swapped in.
- **No authn/authz, rate limiting, or TLS** on the API.
- **Secret handling is a placeholder.** Values are passed through verbatim into
  an Opaque Secret; production should integrate a real secret manager
  (External Secrets, Vault, sealed-secrets).
- **`git_ref` resolves branches and tags**, not arbitrary commit SHAs (a shallow
  single-branch clone limitation).
- **Ingress host** (`demo-app.local`) needs a hosts-file entry or `Host:` header
  to reach via the ingress controller; `kubectl port-forward` works without it.
- **Single replica of the controller** — no leader election; not HA.

---

## License

MIT — see [`LICENSE`](LICENSE).
