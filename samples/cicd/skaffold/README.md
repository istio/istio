# Skaffold

This is intended for demonstration only, and is not tuned for performance or security.

skaffold is a tool that enables fast development iteration and controls deployment to local or remote clusters

If running `skaffold run` for deployment, manifests are pulled from remote charts, if running `skaffold dev` for development and hot reload, manifests are pulled from current branch.

## Quick Start

skaffold is built around modules and profiles

1) istio-base + istio

    ```bash
    skaffold run -m istiod
    ```

2) istio-base + istio + ingress

    ```bash
    skaffold run -m ingress
    ```

3) istio-base + istio + ingress + kiali

    ```bash
    skaffold run -m ingress,kiali
    ```

4) istio-base + istio + ingress + kiali + bookinfo

    ```bash
    skaffold run -m ingress,kiali,bookinfo
    ```

## References

- Github: [github.com/GoogleContainerTools/skaffold](https://github.com/GoogleContainerTools/skaffold)
- Site: [skaffold.dev](https://skaffold.dev/)

### TODO

- Add build and test stage for images in istiod (pilot and proxy)
- Addons
