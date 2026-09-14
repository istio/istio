# Sidecar to Ambient

This guide walks through migrating a sidecar-based Istio deployment with L7 traffic policies
to ambient mode with zero downtime. It uses a `sleep` client calling a `helloworld`
service with two versions (v1/v2) and a 50/50 traffic split.

It relies on a workload attached waypoint to handle L7 traffic without applying traffic policies twice during the migration.

## Prerequisites

- Istio installed with the **ambient** profile (istiod, ztunnel, istio-cni)
  ```bash
  helm install istio-base manifests/charts/base -n istio-system --create-namespace --wait
  # Enabling access logging makes it easy to verify traffic is flowing through the waypoint.
  helm install istiod manifests/charts/istio-control/istio-discovery/ --namespace istio-system --set profile=ambient --set meshConfig.accessLogFile=/dev/stdout --wait
  helm install istio-cni manifests/charts/istio-cni -n istio-system --set profile=ambient --wait
  helm install ztunnel manifests/charts/ztunnel -n istio-system --wait
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

### Verify

Confirm client can talk to server

```bash
kubectl exec deploy/sleep -n client -- curl -s helloworld.server:5000/hello
```

## Step 1: Deploy Waypoint

Creating a waypoint with the label `istio.io/waypoint-for: all` will allow the waypoint to handle both service and workload traffic. Before the client is migrated to ambient, traffic will pass through the waypoint destined for the workload(s) and after the client is migrated to ambient it will be destined for the service.

### 1a. Create workload waypoint Gateways

Create a waypoint for the server.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: helloworld-waypoint
  namespace: server
  labels:
    istio.io/waypoint-for: all
spec:
  gatewayClassName: istio-waypoint
  listeners:
    - name: mesh
      port: 15008
      protocol: HBONE
```

### 1b. Label workloads to use the waypoint

The `istio.io/use-waypoint` label must be on the **workloads** (pods), not the
Services. Add the label to the pod template in each Deployment:

```bash
kubectl patch deployment helloworld-v1 -n server --type merge -p '
  {"spec":{"template":{"metadata":{"labels":{"istio.io/use-waypoint":"helloworld-waypoint"}}}}}'

kubectl patch deployment helloworld-v2 -n server --type merge -p '
  {"spec":{"template":{"metadata":{"labels":{"istio.io/use-waypoint":"helloworld-waypoint"}}}}}'
```

Traffic won't flow through the waypoint until the server is migrated to ambient.

### Verify

```bash
# Waypoint pods should be Running and Gateways should show PROGRAMMED=True
kubectl get pods -n server
kubectl get gateway -n server

# Confirm workloads have waypoints assigned
istioctl ztunnel-config workloads | grep helloworld

# Confirm client can talk to server
kubectl exec deploy/sleep -n client -- curl -s helloworld.server:5000/hello
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
```

### 2d. Label the Service

After the client is migrated to Ambient, traffic should continue flowing through the waypoint destined for the Service.

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

kubectl exec deploy/sleep -n client -- curl -s helloworld.server:5000/hello

# Service waypoint should now handle traffic
kubectl logs deploy/helloworld-waypoint -n server --tail=5
```

## Step 4: Clean Up

With all traffic destined for the service, you can remove the the workload waypoint labels.

### 4a. Remove waypoint labels from workloads

```bash
kubectl patch deployment helloworld-v1 -n server --type merge -p '
  {"spec":{"template":{"metadata":{"labels":{"istio.io/use-waypoint":null}}}}}'

kubectl patch deployment helloworld-v2 -n server --type merge -p '
  {"spec":{"template":{"metadata":{"labels":{"istio.io/use-waypoint":null}}}}}'
```
