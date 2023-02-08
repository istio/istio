# Dual Stack Support in Istio

Dual-stack support in Kubernetes has been around since 1.16 (alpha). It went beta in 1.21 and is now considered stable as of 1.22. Cloud providers are increasingly providing support for dual-stack IPv4/IPv6 Kubernetes clusters. The dual stack feature for Istio is almost ready in this `experimental-dual-stack` branch and this documentation will describe how to enable it, including the image building and installation with dual stack feature enabled.

In this README:

- [Image building](#image-building)
- [Installation](#installation)
- [Who are using the feature](#who-are-using-the-feature)
- [Others](#others)

In addition, here are some other information you may wish to read:

- [Dual Stack Proposal](https://docs.google.com/document/d/1oT6pmRhOw7AtsldU0-HbfA0zA26j9LYiBD_eepeErsQ/edit#heading=h.se6wjxq9mtpk) - describes the whole design and solution for dual stack feature in Istio
- [Dual Stack Slack](#dual-stack-support) - there are many enterprise and personal users in this slack channel, we would communicate some news or progresses on dual stack in it.

## Image building

Jacob's working on here.

Note:

- User can build necessary binary via command `make build`

- User can build image in your local envrionment via command `make docker`, `make docker.pilot` and `make docker.proxyv2`, etc.

## Installation

There are 2 installation approaches will be introduced in this section:

- **Install via istioctl** - It's very easy for user who install Istio via istioctl to enable dual stack feature support,
    just set the environment variables of both `ISTIO_DUAL_STACK` and `PILOT_USE_ENDPOINT_SLICE` as true, the command may be below:

    - istioctl install --set values.pilot.env.ISTIO_DUAL_STACK=true --set values.pilot.env.PILOT_USE_ENDPOINT_SLICE=true -y

- **Install via helm** - Many enterprise users may choose helm as the installation tool, however, it's similar with using `istioctl`.
    Both `ISTIO_DUAL_STACK` and `PILOT_USE_ENDPOINT_SLICE` are required to set true, and the command may be below:

    - helm install istiod manifests/charts/istio-control/istio-discovery \
        --set global.imagePullPolicy="Always" \
        --set global.hub="<YOUR-IMAGE-HUB>" \
        --set global.tag="<YOUR-IMAGE-TAG>" \
        --set pilot.env.ISTIO_DUAL_STACK=true \
        --set pilot.env.PILOT_USE_ENDPOINT_SLICE=true \
        ...... \
        -n istio-system

> Note: For environment variable PILOT_USE_ENDPOINT_SLICE, it will be true by default for some kubernetes version, such as 1.21+.

## Who are using the feature

- **Ecloud**

  As China Mobile's strategic platform, [Ecloud](https://ecloud.10086.cn/home/) is currently the fastest-growing public cloud in China.
  As a mainstream project of service mesh, Istio has been widely used by public cloud products in Ecloud for canary release, network resilience and observability.

## Others

### Some Questions for Dual Stack feature enable

- **Q: Is it available with dual stack feature enable if Istio CNI is enabled?**

> A: Yes, it is available if user want to use istio CNI based on current implementation.

- **Q: Is it available with dual stack feature enable if Kubernetes CNI enables IPVS?**

> A: In theory yes, we have verified that the Calico with IPVS enabled is okay for now.

- **Q: Which release of Istio is based on for this dual stack feature?**

> A: This extra branch is based on 1.13 release of Istio.

...... looking forward to more for this!

- **Q: Is there any verification for this branch code?**

> A: Only basic verification has been completed, however, the unit test and integration test is unavailable for now and both these 2 sections will be going on.
