# WIP: Windows Support

## Building

On unix systems, you'll need a Windows toolchain to build [ztunnel](https://github.com/istio/ztunnel/blob/3d2938e19f0f6da04005556cc015bf34c299e16b/WINDOWS.md) at minimum and istio-cni if you're using CGO. Probably good to install it just for good measure. MinGW is one of the best places to start (Cygwin also exists, but haven't tested it). You can install MinGW on Ubuntu with:

```bash
sudo apt-get install mingw-w64
```

And then for Go you're done. You can cross-compile with:

```bash
make -e TARGET_OS=windows build-cni
```

## Creating Container Images

Docker does support cross-building for Windows, but it is a bit of a pain. You can use the `docker buildx` command to build images for Windows. First, you need to create a new builder instance:

```bash
docker buildx create --name windows-builder --platform=windows/amd64 # change to windows/arm64 if you want to build for arm64
```

Then, you can build the istio-cni image using the `docker buildx` command. Again, you need to specify the platform as `windows/amd64` or `windows/arm64` depending on your target architecture.

```bash
docker buildx build . -f cni/deployments/kubernetes/Dockerfile.install-cni-windows --pull --platform=windows/amd64 --output type=registry -t localhost:5000/istio-cni-windows --builder windows-builder
```

This will build the image and push it to the local registry. This isn't very useful in practice though (unless you're on a windows machine) because kind, k3s, minikube, and the like don't support Windows containers. You'll probably need to push it to a remote registry and pull it down to a Kubernetes cluster with Windows nodes (likely in a cloud provider). There's a sample manifest in the root path of this branch. Converting it to Istio's helm chart format is a TODO.
