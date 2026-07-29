# Sidecar to Ambient

This guide walks through migrating a sidecar-based Istio deployment to ambient mode
with zero downtime. It uses a `sleep` client calling a `helloworld`
service with two versions (v1/v2) and a 50/50 traffic split.

It uses a workload waypoint for each version of the service.

## Prerequisites

- Istio installed with the **ambient** profile (istiod, ztunnel, istio-cni)
  ```bash
  helm install istio-base istio/base -n istio-system --create-namespace --wait
  helm install istiod istio/istiod --namespace istio-system --set profile=ambient --wait
  helm install istio-cni istio/cni -n istio-system --set profile=ambient --wait
  helm install ztunnel istio/ztunnel -n istio-system --wait
  ```
- Gateway API CRDs installed:
  ```bash
  kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.2/standard-install.yaml
  ```

## Starting State

Create two namespaces with sidecar injection enabled and deploy the sample
applications:

```bash
kubectl create ns client
kubectl label ns client istio-injection=enabled
kubectl apply -f samples/sleep/sleep.yaml -n client

kubectl create ns server
kubectl label ns server istio-injection=enabled
kubectl apply -f samples/helloworld/helloworld.yaml -n server
```

This gives you:

- **client**: `sleep` deployment (sidecar-injected)
- **server**: `helloworld-v1` and `helloworld-v2` deployments (sidecar-injected)
  with a shared `helloworld` Service

Next, create per-version Services and an HTTPRoute to split traffic 50/50:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: helloworld-v1
  namespace: server
spec:
  selector:
    app: helloworld
    version: v1
  ports:
    - port: 5000
      name: http
---
apiVersion: v1
kind: Service
metadata:
  name: helloworld-v2
  namespace: server
spec:
  selector:
    app: helloworld
    version: v2
  ports:
    - port: 5000
      name: http
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: helloworld
  namespace: server
spec:
  parentRefs:
    - kind: Service
      group: ""
      name: helloworld
      port: 5000
  rules:
    - backendRefs:
        - name: helloworld-v1
          port: 5000
          weight: 50
        - name: helloworld-v2
          port: 5000
          weight: 50
```

## Step 1: Deploy Workload Waypoints

Unlike service waypoints, workload waypoints are safe to use during sidecar-to-ambient interop because they only apply
AuthorizationPolicy and RequestAuthentication. This avoids the double policy issues where both the sidecar and the waypoint would
apply routing rules like retries, timeouts, and fault injection.

### 1a. Create workload waypoint Gateways

Create a waypoint for each workload version:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: helloworld-v1-waypoint
  namespace: server
  labels:
    istio.io/waypoint-for: workload
spec:
  gatewayClassName: istio-waypoint
  listeners:
    - name: mesh
      port: 15008
      protocol: HBONE
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: helloworld-v2-waypoint
  namespace: server
  labels:
    istio.io/waypoint-for: workload
spec:
  gatewayClassName: istio-waypoint
  listeners:
    - name: mesh
      port: 15008
      protocol: HBONE
```

### 1b. Label workloads to use their waypoints

The `istio.io/use-waypoint` label must be on the **workloads** (pods), not the
Services. Add the label to the pod template in each Deployment:

```bash
kubectl patch deployment helloworld-v1 -n server --type merge -p '
  {"spec":{"template":{"metadata":{"labels":{"istio.io/use-waypoint":"helloworld-v1-waypoint"}}}}}'

kubectl patch deployment helloworld-v2 -n server --type merge -p '
  {"spec":{"template":{"metadata":{"labels":{"istio.io/use-waypoint":"helloworld-v2-waypoint"}}}}}'
```

### Verify

```bash
# Waypoint pods should be Running and Gateways should show PROGRAMMED=True
kubectl get pods -n server
kubectl get gateway -n server

# Confirm workloads have waypoints assigned
istioctl ztunnel-config workloads | grep helloworld
```

## Step 2: Migrate Server Namespace to Ambient

### 2a. Switch the namespace label

Remove the sidecar injection label and add the ambient label:

```bash
kubectl label ns server istio-injection- istio.io/dataplane-mode=ambient
```

### 2b. Restart server deployments

The pods need to restart to drop their sidecars:

```bash
kubectl rollout restart deployment helloworld-v1 helloworld-v2 -n server
kubectl rollout status deployment helloworld-v1 helloworld-v2 -n server
```

### Verify

```bash
# Pods should now be 1/1 (no sidecar)
kubectl get pods -n server

# Generate traffic to verify connectivity
kubectl exec deploy/sleep -n client -- curl -s helloworld.server:5000/hello

# Waypoint access logs should show traffic flowing through them
kubectl logs deploy/helloworld-v1-waypoint -n server --tail=5
kubectl logs deploy/helloworld-v2-waypoint -n server --tail=5
```

### 2c. Create the service waypoint

You must create the service waypoint before switching the client to Ambient otherwise when the client is updated to ambient it will bypass the workload waypoints.

TODO: Is this the expected behavior? Should sidecars send traffic through workload waypoints but ambient workloads bypass them?

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: helloworld-waypoint
  namespace: server
spec:
  gatewayClassName: istio-waypoint
  listeners:
    - name: mesh
      port: 15008
      protocol: HBONE
```

### 2d. Label the Service

```bash
kubectl label service helloworld istio.io/use-waypoint=helloworld-waypoint -n server
```

## Step 3: Migrate Client Namespace to Ambient

### 3a. Switch the namespace label

```bash
kubectl label ns client istio-injection- istio.io/dataplane-mode=ambient
```

### 3b. Restart client deployment

```bash
kubectl rollout restart deployment sleep -n client
kubectl rollout status deployment sleep -n client
```

### Verify

```bash
# Pod should be 1/1 (no sidecar)
kubectl get pods -n client

# Service waypoint should now handle traffic
kubectl logs deploy/helloworld-waypoint -n server --tail=5

# Traffic should not flow through the workload waypoints
kubectl logs deploy/helloworld-v1-waypoint -n server --tail=5
kubectl logs deploy/helloworld-v2-waypoint -n server --tail=5
```

## Step 4: Clean Up Workload Waypoints

With the service waypoint handling all traffic, the workload waypoints are no longer
needed.

### 4a. Remove waypoint labels from workloads

```bash
kubectl patch deployment helloworld-v1 -n server --type merge -p '
  {"spec":{"template":{"metadata":{"labels":{"istio.io/use-waypoint":null}}}}}'

kubectl patch deployment helloworld-v2 -n server --type merge -p '
  {"spec":{"template":{"metadata":{"labels":{"istio.io/use-waypoint":null}}}}}'
```

### 4b. Delete the workload waypoint Gateways

```bash
kubectl delete gateway helloworld-v1-waypoint helloworld-v2-waypoint -n server
```

### Verify

```bash
# Only the service waypoint should remain
kubectl get gateway -n server

# Traffic continues flowing through the service waypoint
kubectl logs deploy/helloworld-waypoint -n server --tail=5
```
