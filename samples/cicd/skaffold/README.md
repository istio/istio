# Skaffold

skaffold is a tool that enables fast development iteration and controls deployment to local or remote clusters

If running `skaffold run` for deployment, manifests are pulled from remote charts, if running `skaffold dev` for development and hot reload, manifests are pulled from current branch.

## Quick Start

skaffold is built around modules and profiles

1) Istio-Base + Istio (default)

    ```bash
    skaffold run
    ```

2) Istio-Base + Istio + Ingress

    ```bash
    skaffold run -m ingress
    ```

3) Istio-Base + Istio + Ingress + Kiali

    ```bash
    skaffold run -m ingress,kiali
    ```

## Refences

- Github: [github.com/GoogleContainerTools/skaffold](https://github.com/GoogleContainerTools/skaffold)
- Site: [skaffold.dev](https://skaffold.dev/)

### TODO

- Add build and test stage for images in istio d(pilot and proxy)
- Addons and portForword
- Add bookinfo
