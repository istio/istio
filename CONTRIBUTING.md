# Contribution guidelines

So you want to hack on Istio? Yay! Please refer to Istio's overall
[contribution guidelines](https://github.com/istio/community/blob/master/CONTRIBUTING.md)
to find out how you can help.

The contents below is copied from the old readme for building/running/testing ambient from source:

## Build

```shell
./local-test-utils/kind-registry.sh

export ISTIO_ENVOY_BASE_URL=https://storage.googleapis.com/solo-istio-build/proxy
HUB=gcr.io/xyz # consider localhost:5000
TAG=ambient
export GOPRIVATE=github.com/solo-io/istio-api-sidecarless

# If using git with ssh key, you can use this:
# git config --global url."ssh://git@github.com/solo-io".insteadOf https://github.com/solo-io

# Build Istiod and proxy
tools/docker --targets=pilot,proxyv2,app,install-cni --hub=$HUB --tag=$TAG --push # consider --builder=crane
```

## Cluster Setup and Install

```shell
./local-test-utils/reset-kind.sh

# Configure cluster to use the local registry
./local-test-utils/kind-registry.sh

# Move images from your remote registry to the local one (if needed) - not needed if building and pushing images to localhost.
./local-test-utils/refresh-istio-images.sh
```

## Setup Ambient

```shell
# Mesh config options are optional to improve debugging
CGO_ENABLED=0 go run istioctl/cmd/istioctl/main.go install -d manifests/ --set hub=$HUB --set tag=$TAG -y \
  --set profile=ambient --set meshConfig.accessLogFile=/dev/stdout --set meshConfig.defaultHttpRetryPolicy.attempts=0 \
  --set values.global.imagePullPolicy=Always

kubectl apply -f local-test-utils/samples/
```

## New Test with Ambient

```shell
# Label the default namespace to make it part of the mesh
kubectl label namespace default istio.io/dataplane-mode=ambient

kubectl exec -it deploy/sleep -- curl http://helloworld:5000/hello

# In a separate shell, start an interactive session on the client pod
k exec -it $(k get po -lapp=sleep -ojsonpath='{.items[0].metadata.name}') -- sh

# (From the client pod) Send traffic
curl helloworld:5000/hello
```

## Tests with Sidecar Continue to Work

```shell
# Label the foo namespace with istio injection
kubectl create ns foo
kubectl label namespace foo istio-injection=enabled

# Deploy and test the sample with sidecars
kubectl apply -f local-test-utils/samples/ -n foo
kubectl exec -it deploy/sleep -n foo -- curl http://helloworld:5000/hello

# Test ambient to sidecar and sidecar to ambient:
kubectl exec -it deploy/sleep  -- curl  http://helloworld.foo:5000/hello
kubectl exec -it deploy/sleep -n foo -- curl  http://helloworld.default:5000/hello
```

## Debugging

Turning on debug logs

```shell
WORKER1=$(kubectl -n istio-system get pods --field-selector spec.nodeName==ambient-worker -lapp=ztunnel -o custom-columns=:.metadata.name --no-headers)
WORKER2=$(kubectl -n istio-system get pods --field-selector spec.nodeName==ambient-worker2 -lapp=ztunnel -o custom-columns=:.metadata.name --no-headers)

kubectl -n istio-system port-forward $WORKER1 15000:15000&
kubectl -n istio-system port-forward $WORKER2 15001:15000&

curl -XPOST "localhost:15000/logging?level=debug"
curl -XPOST "localhost:15001/logging?level=debug"

kubectl -n istio-system logs -lapp=ztunnel -f
# Or,
kubectl -n istio-system logs $WORKER1 -f
kubectl -n istio-system logs $WORKER2 -f

curl "localhost:15000/config_dump"
```

## Run the tests

Note: We have to use the custom image to allow installing `ipsets`.

```shell
INTEGRATION_TEST_FLAGS="--istio.test.ambient" prow/integ-suite-kind.sh \
  --kind-config prow/config/ambient-sc.yaml --node-image kindest/node:v1.24.0 \
  test.integration.ambient.kube
```

A workaround for private repo in-containers:

```shell
# add a replace directive for istio/api to ../api
# clone/checkout ambient branch in api repo
# run with CONDITIONAL_HOST_MOUNTS
CONDITIONAL_HOST_MOUNTS="--mount type=bind,source=$(cd ../api && pwd),destination=/api" \
INTEGRATION_TEST_FLAGS="--istio.test.ambient" prow/integ-suite-kind.sh \
  --kind-config prow/config/ambient-sc.yaml --node-image kindest/node:v1.24.0 \
  test.integration.ambient.kube
```

Run integration tests locally on KinD cluster:

```shell
# spin up kind cluster using existing scripts
./local-test-utils/reset-kind.sh

# tell integration test framework which cluster to use
export KIND_NAME=ambient

# run integation tests
# rely on HUB and TAG env vars being set and docker images built using steps above
# use -v to get live output during test run
# use -run to execute a specific test: i.e. -run "TestServices"
# skip test cleanup in order to debug state with --istio.test.nocleanup
go test -tags=integ ./tests/integration/ambient/... --istio.test.ambient  --istio.test.ci -p 1
```

## EKS specific notes

`kubectl version` against working setup:

```shell
Server Version: version.Info{Major:"1", Minor:"21+", GitVersion:"v1.21.12-eks-a64ea69", GitCommit:"d4336843ba36120e9ed1491fddff5f2fec33eb77", GitTreeState:"clean", BuildDate:"2022-05-12T18:29:27Z", GoVersion:"go1.16.15", Compiler:"gc", Platform:"linux/amd64"}
```

`kg nodes` against working setup (node version only):

```shell
v1.21.12-eks-5308cf7
```

newer versions appear to be slightly broken (same node works, cross node request to other envoy looks malformed), such as

```shell
v1.22.6-eks-7d68063
```

