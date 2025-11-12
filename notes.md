# Notes

## Prep Steps

1. launch a codespace from your fork of istio/istio
1. checkout a feature branch from master, call it what you'd like. Perhaps `contribfest`

```shell
export TAG=1.29-alpha.5dcad23c9e9086eadce05381890a29dea3f97fb6
export HUB=gcr.io/istio-testing
./prow/integ-suite-kind.sh --skip-cleanup --skip-build
```

## Install Istio

```shell
# Build istioctl binary
make -B $(pwd)/out/linux_amd64/release/istioctl-linux-amd64
# Convenience alias
alias istioctl="$(pwd)/out/linux_amd64/release/istioctl-linux-amd64"
istioctl install --set profile=ambient --skip-confirmation --set tag=$TAG --set hub=$HUB
# waypoints are Kubernetes Gateway API Resources, install the CRDs
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml
```

## Setup and test a basic playground

```shell
kubectl create namespace server
kubectl label namespace server istio.io/dataplane-mode=ambient
kubectl apply -f ./samples/httpbin/httpbin.yaml -n server
kubectl label -n server svc httpbin istio.io/use-waypoint=waypoint
kubectl create namespace client
kubectl label namespace client istio.io/dataplane-mode=ambient
kubectl apply -f ./samples/sleep/sleep.yaml -n client
```

check the environment

```shell
kubectl exec -n client deploy/sleep -it -- curl -v http://httpbin.server:8000/headers
kubectl logs -n istio-system ds/ztunnel | grep outbound | tail -1
```

(optionally) observer the ztunnel config using, `istioctl zc all -o yaml | less`

## Observe the happy path

```shell
istioctl waypoint apply -n server
istioctl waypoint status -n server --waypoint-timeout=10s
```

## Recreate the bug

```shell
# reset
istioctl waypoint delete -n server waypoint
# scale down istiod so nothing programs waypoints
kubectl scale deploy/istiod -n istio-system --replicas=0
# try again
istioctl waypoint apply -n server
# this time we don't wait for a long time though, we know it's not going to be programmed by anything
istioctl waypoint status -n server --waypoint-timeout=10s
# observe the problem
```

## A reasonable development loop

```shell
# change istioctl somehow
# hint, look in istioctl/pkg/waypoint/waypoint.go
# build istioctl
make -B $(pwd)/out/linux_amd64/release/istioctl-linux-amd64
# test istioctl
istioctl waypoint status -n server --waypoint-timeout=10s --my-fixed-flag=my-flag-setting
```

## BONUS CONTENT: On building your own Istio images

In a Codespace, building your own Istio images can be slow. On the order of 10 minutes or more depending on your machine type. We've chosen to skip this step and instead use a 1.29 dev build created by istio's CI whenever a PR merges to the `master` branch. This doesn't mean that building an Istio image is difficult though and it's a good skill to understand if you are looking to work on other portions of the codebase, such as pilot (istiod). In this section we'll talk briefly about how to build and run your own images. If you are looking for deeper post-contribfest learning, we suggest taking a look at this section. Happy coding :)

First, Let's adjust those environment variables which control what we're building and installing.

```shell
export TAG=istio-testing
export HUB=localhost:5000
export DOCKER_TARGETS='docker.pilot docker.proxyv2 docker.ztunnel docker.install-cni'
export DOCKER_BUILD_VARIANTS="distroless"
```

Next, setup a new kind cluster. This time we won't skip build though:

```shell
./prow/integ-suite-kind.sh --skip-cleanup
```

The tag here could be whatever you want, but the HUB is rather interesting. When you used these arguments for prow script to build you KinD envionment, you may have noticed there's an extra registry container running in docker. This registry image let's you build and push images to your local development environment. It requires a little bit of special plumbing in KinD which is also taken care of for you by our prow scripts.

Now, let's build our images. The script did already build some images, but it didn't follow all of our directives so we'll build or own now. We set the desired target's above to be simply the core images needed for a basic ambient mesh installation. There are other images which would normally be built that enable our integration tests, but for now we can reduce the number of images so this progresses a bit faster.

```shell
make docker.push
```

When you run this you'll get a bit of information about what it's going to try to do, and it'll handle building and pushing images for you.

```text
2025-11-07T13:44:17.623450Z     info    Args: Push:              true
Save:              false
NoClobber:         false
NoCache:           false
Targets:           [pilot proxyv2 ztunnel install-cni]
Variants:          [distroless]
Architectures:     [linux/amd64]
BaseVersion:       master-2025-10-01T19-01-35
BaseImageRegistry: gcr.io/istio-release
ProxyVersion:      057d1f084c2c3e3fadb4dbd51edd3e97a963b78b
ZtunnelVersion:    8ad92ad8d411e7ea63913c9b650dedb684d5ba6e
IstioVersion:      1.29-dev
Tags:              [istio-testing]
Hubs:              [localhost:5000]
Builder:           docker
```

Notice that it read tags, hubs, targets and variants correctly from environment variables

The next step would be installing using these images. If Istio is already installed in your local KinD, you could edit deployments and daemon sets to use these images.

```shell
istioctl install --set profile=ambient --skip-confirmation --set tag=$TAG --set hub=$HUB --set values.global.imagePullPolicy=Always
```

**PRO TIP:** by default, we'll install using the `IfNotPresent` imagePullPolicy. When building your local development loop, this can cause you some frustrations. After the first time a pod starts, the `istio-testing` tag will be present on the node and Kubernetes will stop pulling updated images when you push them! It's strongly advised to change this to `Always`. Notice, in the install command shown above we're taken care of this setting.

From here, you are ready to start a development loop by making changes, running `make docker.push`, and restarting the containers you're working on. Give it a try. `newDiscoveryCommand` in `pilot/cmd/pilot-discovery/app/cmd.go` is the entry point for our istiod image. Try adding a log message to it's RunE and then check out the logs after your newly built istiod starts up. You can tighten this loop further by only building the images you're working on now, but I'll leave this as an excercise for the reader to figure out.
