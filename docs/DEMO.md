# End-to-end demo

A captured run of `vibsl` against a local **kind** cluster (Kubernetes v1.32)
with a local registry on `localhost:5001`, ingress-nginx, and metrics-server.

Environment: Go 1.26, Docker 29.5, kind v0.27, Arch Linux.

---

## 1. Bring up the cluster + registry

```console
$ make cluster-up
Creating cluster "vibsl" ...
 ✓ Ensuring node image (kindest/node:v1.32.2)
 ✓ Preparing nodes
 ✓ Starting control-plane
 ✓ Installing CNI
 ✓ Installing StorageClass
Set kubectl context to "kind-vibsl"
kind cluster 'vibsl' + registry 'kind-registry' on :5001 ready.
```

## 2. Start the API server

```console
$ make run
config: workers=2 queue=64 workdir=/tmp/vibsl-jobs registry=localhost:5001 dry_run_k8s=false
kubernetes: server-side apply enabled (field manager "vibsl-controller")
listening on :8080
```

## 3. Submit the sample job

```console
$ curl -sS -X POST localhost:8080/jobs -H 'Content-Type: application/json' --data @examples/job.json
{
  "id": "2329e734061fb028a25e3f7b7ea775f3",
  "spec": { "name": "demo-app", "repo_url": "https://github.com/Dhruva430/vibsl-sample-app.git", ... },
  "phase": "pending",
  "message": "queued",
  ...
}
```

## 4. Pipeline log (`GET /jobs/{id}` → `.logs`)

```text
05:16:30  [pending  ] queued
05:16:30  [cloning  ] cloning https://github.com/Dhruva430/vibsl-sample-app.git
05:16:31  [cloning  ] Enumerating objects: 6, done.
05:16:31  [cloning  ] Total 6 (delta 0), reused 6 (delta 0), pack-reused 0
05:16:31  [cloning  ] checked out d68586401b4b3d9371b1d12702fb8c31b46e8299
05:16:31  [building ] building localhost:5001/demo-app:v1
05:16:31  [building ] $ docker build --file .../Dockerfile --tag localhost:5001/demo-app:v1 ...
05:16:31  [building ] Step 1/12 : FROM golang:1.23-alpine AS build
05:16:32  [building ] Step 6/12 : RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/app .
05:16:38  [building ] Step 7/12 : FROM gcr.io/distroless/static:nonroot
05:16:38  [building ] Step 12/12 : ENTRYPOINT ["/app"]
05:16:38  [building ] Successfully tagged localhost:5001/demo-app:v1
05:16:38  [pushing  ] pushing localhost:5001/demo-app:v1
05:16:38  [pushing  ] $ docker push localhost:5001/demo-app:v1
05:16:38  [pushing  ] v1: digest: sha256:50996aad8970b7fd6ffd27e738799414b38719e3ca2c66068846083d72bbc1e8 size: 3232
05:16:38  [deploying] rendering and applying manifests
05:16:38  [deploying] applied Namespace/demo
05:16:38  [deploying] applied ServiceAccount/demo/demo-app
05:16:38  [deploying] applied ConfigMap/demo/demo-app-config
05:16:38  [deploying] applied Secret/demo/demo-app-secret
05:16:38  [deploying] applied Role/demo/demo-app
05:16:38  [deploying] applied RoleBinding/demo/demo-app
05:16:38  [deploying] applied Deployment/demo/demo-app
05:16:38  [deploying] applied Service/demo/demo-app
05:16:38  [deploying] applied Ingress/demo/demo-app
05:16:38  [deploying] applied HorizontalPodAutoscaler/demo/demo-app
05:16:38  [deploying] applied PodDisruptionBudget/demo/demo-app
05:16:38  [deploying] applied NetworkPolicy/demo/demo-app
05:16:38  [succeeded] deployed localhost:5001/demo-app:v1
```

Clone → build → push → apply of all 12 resources completed in ~8 seconds
(image layers cached; first run ~40s for the Go build + base image pulls).

## 5. What landed in the cluster

```console
$ kubectl get deploy,pod,svc,ingress,hpa,pdb,networkpolicy,sa,configmap,secret,role,rolebinding -n demo

NAME                       READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/demo-app   2/2     2            2           5m

NAME                           READY   STATUS    RESTARTS   AGE
pod/demo-app-97fb8d67f-bpvc4   1/1     Running   0          63s
pod/demo-app-97fb8d67f-tdt6r   1/1     Running   0          74s

NAME               TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)   AGE
service/demo-app   ClusterIP   10.96.101.101   <none>        80/TCP    5m

NAME                                 CLASS   HOSTS            ADDRESS     PORTS   AGE
ingress.networking.k8s.io/demo-app   nginx   demo-app.local   localhost   80      5m

NAME                                           REFERENCE             TARGETS       MINPODS   MAXPODS   REPLICAS
horizontalpodautoscaler.autoscaling/demo-app   Deployment/demo-app   cpu: 3%/70%   2         5         2

NAME                                  MIN AVAILABLE   MAX UNAVAILABLE   ALLOWED DISRUPTIONS   AGE
poddisruptionbudget.policy/demo-app   1               N/A               1                     5m

NAME                                       POD-SELECTOR                                                         AGE
networkpolicy.networking.k8s.io/demo-app   app.kubernetes.io/managed-by=vibsl,app.kubernetes.io/name=demo-app   5m

serviceaccount/demo-app                          (token automount disabled)
configmap/demo-app-config    DATA 1
secret/demo-app-secret       Opaque   DATA 1
role.rbac.authorization.k8s.io/demo-app
rolebinding.rbac.authorization.k8s.io/demo-app   Role/demo-app
```

All 12 required resources present; Deployment healthy at 2/2; HPA reading live
CPU from metrics-server; PDB permitting 1 disruption.

## 6. The app actually serves traffic

Through the generated **Ingress** (kind maps `:80` → host `:8081`):

```console
$ curl -s -H 'Host: demo-app.local' http://localhost:8081/
Hello from vibsl — served by demo-app-97fb8d67f-bpvc4

$ curl -s -H 'Host: demo-app.local' http://localhost:8081/healthz
ok
```

The greeting text (`Hello from vibsl`) comes from the **ConfigMap** the service
generated, wired into the pod via `envFrom`. The image was pulled from the
local registry (`localhost:5001/demo-app:v1`, ~3 MB distroless).

## 7. Resubmission = reconcile

Re-`POST`ing the same job server-side-applies over the existing objects (no
"already exists" errors) and triggers a rolling update:

```console
deployment "demo-app" successfully rolled out
```
