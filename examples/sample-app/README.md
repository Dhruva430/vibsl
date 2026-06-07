# vibsl-sample-app

A minimal Go HTTP server used as the demo workload for
[vibsl](https://github.com/Dhruva430/vibsl) — a queue-based Docker build and
Kubernetes deploy API.

- Listens on `$PORT` (default `8080`)
- `GET /` → greeting (from `$GREETING`, supplied via the generated ConfigMap)
- `GET /healthz` → `ok`

Builds into a static, non-root, distroless image (see `Dockerfile`), so it runs
under the hardened Deployment vibsl generates (`runAsNonRoot`,
`readOnlyRootFilesystem`, all capabilities dropped).

## Run locally

```bash
go run .
# in another shell
curl localhost:8080/
```

## Build the image

```bash
docker build -t vibsl-sample-app:dev .
docker run --rm -p 8080:8080 -e GREETING="hi" vibsl-sample-app:dev
```
