# Experimental Windows Ambient Support

This is an experimental branch that adds support for Windows user nodes for the ambient data-plane mode (there's no support for sidecar mode on Windows at the moment). At the moment, the only way to use this experimental support is building the artifacts necessary from this branch. This document assumes some familiarity with Istio. Fore more details about Istio as whole, please go to the [main branch](https://github.com/istio/istio).

⚠️ **Do not use this branch in production**, it hasn't been extensively tested and it has a couple of known issues.

## List of Known Issues
- We only support amd64 Windows nodes for now.
- In some edge cases, containers may fail during their startup but won't be removed.
- Ztunnel can't intercept DNS requests yet.

## How It Works

The Istio components running on Windows are only two: ztunnel and the CNI plugin. Everything else still runs on Linux nodes, including `istiod` and Waypoint Proxies when added to the mesh.

## Building the Experimental Windows Support

At the moment we don't have ready to use Windows images, so you have to build your own. Those are the images pulled by the Istio deployument, which includes, `istiod`, ztunnel, the CNI plugin and others. Once you have built those images, you'll also have to host them in a container registry of your choice. After you have those images pushed to a registry, you can build `istioctl` and run `istioctl install` with the necessary parameters (more details on the following sections).

### Dependencies

First things first, you have to install the dependencies (which includes Docker) as described in [Preparing for Development](https://github.com/istio/istio/wiki/Preparing-for-Development).

### Building and Storing Your Own Images

Pull the experimental branch for both Istio and ztunnel in your Linux machine.

```
git clone -b experimental-windows-ambient git@github.com:istio/istio.git istio
git clone -b experimental-windows-ambient git@github.com:istio/istio.git ztunnel
```

The build process is pretty similiar to the one in the production branch. The difference is a few shell variables we need set:

The container registry we're pushing the images to. Make sure you're authorized in Docker to push images to the registry of your choice.
```
export HUB=example.registry.io
```

The target operating systems and architectures. We need both Linux and Windows here, as we're also building `istiod` and other components that run on Linux system nodes. If you're looking only for the Windows targets (ztunnel and CNI plugin), few free to use only `windows/amd64`.
```
export DOCKER_ARCHITECTURES=linux/amd64,windows/amd64
```

Some extra mount parameters used by the container hosting the build. That makes sure the ztunnel repository is available inside the host container.
```
export CONDITIONAL_HOST_MOUNTS="--mount type=bind,source=/home/gustavomeira/repos/ztunnel,destination=/ztunnel"
export BUILD_ZTUNNEL_REPO=/ztunnel
```

Then run:
```
make docker.push
```

If everything goes well, the images should now be available in your container registry. They can now be used by the Istio deployment. Otherwise, please open an issue in this GitHub repository, we're eager to get things going.

### Building `istioctl`

We need `istioctl` to deploy Istio. In order to build it, start by pulling the experimental branch (not necessary you've already done that while building your own images).
```
git clone -b experimental-windows-ambient git@github.com:istio/istio.git istio
cd istio/istioctl/cmd/istioctl/
go build
```

If the build is successful, the generated binary should be available at `istio/istioctl/cmd/istioctl/istioctl`. You can also call it directly with `go run ./istioctl/cmd/istioctl` from inside `istio` if desired.

### Installation

In order to install the Windows ambient mode with `istioctl`, run:
```
istioctl install --set profile=ambient-windows
```

If you built the images yourself, also add `--set hub=example.registry.io`.

## Contributing

If you want to contribute to ambient support for Windows, please follow Istio's contributor guidelines. We also have a [channel on Istio's Slack dedicated to Windows support discussions](https://istio.slack.com/archives/C08KEE4H8CX).

