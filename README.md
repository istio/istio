# Table of Contents

- [Introduction](#introduction)
- [Build](#build)
- [Install](#install)
- [Uninstall](#uninstall)
- [Example](#example)
- [License](#license)

# Introduction
The east-west traffic in Istio is carried by envoy sidecars, which are deployed in every pod together with the application containers. Sidecars provide the functions of secure service-to-service communication, load balancing for various protocols, flexible traffic control and policy, and complete tracing.

However, there are also a few disadvantages of sidecars. First of all, deploying one sidecar to every pod can be resource-consuming and introduce complexity, especially when the number of pods is huge. Not only must those resources be provisioned to the sidecars, but also the control plane to manage the sidecar and to push configurations can be demanding. Second, a query needs to go through two sidecars, one in the source pod and the other one in the destination pod,  in order to reach the destination. For delay-sensitive applications, sometimes, the extra time spent in the sidecars is not acceptable.

We noted that, for the majority of our HTTP applications, many of the rich features in sidecars are unused. That's why we want to propose a light-weighted way to serve east-west traffic without the drawbacks mentioned in the previous paragraph. Our focus is on the HTTP applications that do not require advanced security features, like mTLS.

We propose the centralized east-west traffic gateway, which moves the sidecars and the functionalities they carry to nodes that are dedicated for sidecars, and no application container shares those nodes. This way, no modifications are required on the nodes, and we can save on the resources and the delay. In addition, we can decouple the network management from application management,  and also avoid the resource competition between application and networking. However, because we move the sidecars out of the nodes of applications, we at the same time lose some of the security and tracing abilities provided by the original sidecars. Our observation is that the majority of our applications do not require those features.

# Build

Build binary
```
make build
```
Build acmg docker image
```
make docker.acmg
```

# Install

1. Enter the directory ./out/$(arch)/
```
cd ./out/$(arch)/
```

2. Install acmg profile
```
istioctl install --set profile=acmg
```

# Uninstall

1. Enter the directory ./out/$(arch)/
```
cd ./out/$(arch)/
```

2. Uninstall acmg profile
```
istioctl uninstall --purge -y
```

# Example
```
cd samples/helloworld-acmg
```
```
kubectl apply -f helloworld.yaml
kubectl apply -f helloworld-virtualservice.yaml
```

# License
[Apache License 2.0](./LICENSE)

