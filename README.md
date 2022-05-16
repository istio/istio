# Sidecarless Mesh

## Build

```shell
./local-test-utils/kind-registry.sh

export ISTIO_ENVOY_BASE_URL=https://storage.googleapis.com/solo-istio-build/proxy
HUB=localhost:5000 # local registry in docker (helps avoid permissions issues from private registry)
TAG=ambient-oss # note, if you change the tag here, also change it in the daemonset yaml below.
tools/docker --targets=pilot,proxyv2 --hub=$HUB --tag=$TAG --push --builder=crane
```

## Cluster setup (kind)

```shell
# Create (or re-create) cluster
./local-test-utils/reset-kind.sh
# Configure cluster to use the local registry
./local-test-utils/kind-registry.sh

# Move images from your remote registry to the local one (if needed) - not needed if building and pushing images to localhost.
./local-test-utils/refresh-istio-images.sh
```

## Install

```shell
# Install Istio without gateway or webhook
istioctl install --set profile=minimal --set values.global.operatorManageWebhooks=true -d manifests/ --set hub=$HUB --set tag=$TAG

# TODO move this into manifests/ for istioctl install
kubectl apply -f pilot/cmd/uproxy/daemonset.yaml # replace with your hub/tag
# OR
kubectl apply -f pilot/cmd/uproxy/daemonset.kind.yaml

# Turn mesh on
./redirect.sh ambient

# Update pod membership (will move to CNI). can stop it after it does 1 iteration if pods don't change
./tmp-update-pod-set.sh

# Turn mesh off
./redirect.sh ambient clean
```

## Test it out

```shell
# Optionally, make sure the redirection is turned off
./redirect.sh ambient clean

# Deploy helloworld and sleep client
k apply -f local-test-utils/samples/

# In a separate shell, start an interactive session on the client pod
k exec -it $(k get po -lapp=sleep -ojsonpath='{.items[0].metadata.name}') -- sh

# Enable redirection
./redirect.sh ambient

# (From the client pod) Send traffic
curl helloworld:5000/hello
```

# Debugging

Turning on debug logs

```shell
WORKER1=$(kubectl -n istio-system get pods --field-selector spec.nodeName==ambient-worker -lapp=uproxy -o custom-columns=:.metadata.name --no-headers)
WORKER2=$(kubectl -n istio-system get pods --field-selector spec.nodeName==ambient-worker2 -lapp=uproxy -o custom-columns=:.metadata.name --no-headers)

kubectl -n istio-system port-forward $WORKER1 15000:15000&
kubectl -n istio-system port-forward $WORKER2 15001:15000&

curl -XPOST "localhost:15000/logging?level=debug"
curl -XPOST "localhost:15001/logging?level=debug"

kubectl -n istio-system logs -lapp=uproxy -f
# Or,
kubectl -n istio-system logs $WORKER1 -f
kubectl -n istio-system logs $WORKER2 -f

curl "localhost:15000/config_dump"
```