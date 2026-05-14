# Tilt Development Environment for Istio (istiod)

This directory contains support files for [Tilt](https://tilt.dev/)-based local development of the Istio control plane (istiod / pilot-discovery). Tilt watches your Go source code, automatically recompiles, and live-updates the running istiod pod in a local Kind cluster — no manual `docker build` or `kubectl apply` cycles needed.

## Prerequisites

- **[Tilt](https://docs.tilt.dev/install.html)** v0.33+ installed
- **[Kind](https://kind.sigs.k8s.io/)** cluster with a local registry
  - Recommended: use [ctlptl](https://github.com/tilt-dev/ctlptl) to create a cluster + registry in one step:
    ```bash
    ctlptl create registry ctlptl-registry --port=5000
    ctlptl create cluster kind --registry=ctlptl-registry
    ```
- **Go 1.24+** installed
- **`kubectl`** configured to point at the Kind cluster
- **Helm 3** installed (Tilt uses it to template charts)

## Quick Start

From the **Istio repo root** (not this directory):

```bash
tilt up
```

This single command will:

1. Install Istio CRDs via the `istio-base` Helm chart
2. Compile `pilot-discovery` with debug flags (`-gcflags="all=-N -l"`)
3. Build a minimal dev Docker image and push it to the local registry
4. Deploy istiod via Helm to the Kind cluster (`istio-system` namespace)
5. Set up port forwarding:
   - **8080** → debug/readiness endpoints
   - **15010** → gRPC XDS
   - **15014** → Prometheus metrics
6. Watch for Go source changes under `pilot/` and live-reload automatically

## Verifying It Works

```bash
# Check istiod is running
kubectl get pods -n istio-system -l app=istiod

# Hit the readiness endpoint (port-forwarded by Tilt)
curl http://localhost:8080/ready

# View Prometheus metrics
curl http://localhost:15014/metrics
```

## Live Development Workflow

1. Run `tilt up` (or `tilt up --stream` for inline log output)
2. Open the Tilt UI at **http://localhost:10350** to see resource status
3. Edit any Go file under `pilot/` or `pkg/`
4. Tilt automatically:
   - Detects the file change
   - Recompiles `pilot-discovery` for Linux
   - Syncs the new binary into the running container
   - Restarts the `pilot-discovery` process
5. Check the Tilt UI or terminal for build/deploy status

The inner loop (edit → running code) typically takes a few seconds after the initial build.

## Debugging with Delve

The binary is compiled with `-gcflags="all=-N -l"` (optimizations disabled), so you can attach a debugger:

```bash
# Exec into the istiod pod and start Delve
kubectl -n istio-system exec -it deploy/istiod -- \
  dlv attach $(pgrep pilot-discovery) --headless --listen=:2345 --api-version=2

# In a separate terminal, port-forward Delve
kubectl -n istio-system port-forward deploy/istiod 2345:2345

# Connect from your IDE or Delve CLI
dlv connect localhost:2345
```

> **Note**: You'll need `dlv` installed inside the container or use `kubectl debug` to attach an ephemeral container with Delve.

## Cleanup

```bash
# Tear down all Tilt-managed resources
tilt down

# (Optional) Delete the Kind cluster entirely
ctlptl delete cluster kind-kind
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| **Image pull errors** | Ensure the local registry is running: `docker ps \| grep ctlptl-registry`. The Helm values use `imagePullPolicy=Always`. |
| **CRD-related errors** | Run `tilt down && tilt up` to reinstall CRDs cleanly. |
| **Slow first build** | The initial `go build` compiles the entire binary. Subsequent builds use the Go build cache and are much faster. |
| **"No Kind cluster" error** | Create one with `ctlptl create registry ctlptl-registry --port=5000 && ctlptl create cluster kind --registry=ctlptl-registry`. |
| **Port conflicts** | If ports 8080/15010/15014 are in use, edit the `port_forwards` in the Tiltfile. |

## How It Works

The Tiltfile at the repo root orchestrates:

1. **`helm_resource('istio-base')`** — Installs Istio CRDs from `manifests/charts/base/`
2. **`local_resource('go-compile-istiod')`** — Compiles `pilot-discovery` for Linux, outputs to `tools/tilt/pilot-discovery`
3. **`docker_build_with_restart`** — Builds a minimal Ubuntu-based image with just the binary; on changes, syncs the binary and restarts the process (no full image rebuild)
4. **`helm('istio-discovery')`** — Deploys istiod from `manifests/charts/istio-control/istio-discovery/` with dev overrides
5. **`k8s_resource`** — Configures dependencies and port forwarding

## Scope

This Tiltfile currently covers **istiod (pilot-discovery) only**. It does not manage:
- pilot-agent / sidecar injection
- Istio gateways (ingress/egress)
- CNI plugin
- ztunnel
- istioctl
