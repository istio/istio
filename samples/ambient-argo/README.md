# Ambient Reference Architecture w/ Argo

This repo contains a reference architecture for operating Istio Ambient Mesh with ArgoCD using GitOps.  It demonstrates best practices for leveraging Istio as part of an application platform.

## :boom: DISCLAIMER

Istio Ambient Mesh is still in Alpha, and is not suitable for production use.  Likewise, this reference architecture is of Alpha quality, and includes several rough edges, including:
 * Cluster-Scoped upgrades cause known traffic loss, and have wide blast radius.
 * The tag chart is forked from the primary istio repo, and needs to be merged and published
 * CRDs are not currently upgraded

## Getting Started

This reference architecture assumes that you have an ArgoCD installation with:
 * A [connected cluster](https://argo-cd.readthedocs.io/en/stable/user-guide/commands/argocd_cluster/) named `ambient-cluster`
 * A connection to your repository (for private repos)

To deploy Istio, supporting software, and the bookinfo sample application, copy this folder to the root of your repo and run:

```bash
read -p 'Please enter the URL to your repo:'
OLD_REPO='{repo-placeholder}'
find . \( -type d -name .git -prune \) -o -type f -name '*.yaml' -print0 | xargs -0 sed -i s,$OLD_REPO,$NEW_REPO,g
argocd create application -f meta-application.json
```

## Repository Layout

The meta-application.yaml file is an App-of-Apps that references all other applications needed for running Istio and the demo application via ArgoCD.  The diagram below demonstrates the deployment mechanism for each part of the platform.

![architecture diagram][layout]

## Principles

The [GitOps Principles](https://opengitops.dev/) guide this reference architecture.  With the exception of [./meta-application.yaml](./meta-application.yaml) (the bootstrap file), Git is the source of truth for all components of our application.  Changes to all components of Istio, including the data plane, are initiated with a pull request.  Likewise, rollbacks are as simple as a revert.

In particular, Istio Sidecars are known to violate the Declarative principle - the version of Istio injected in the sidecar is determined at runtime by the version of the injector at the time the pod was created.  Upgrading the injector does not cause the sidecar to upgrade, instead all injected pods must be restarted in order to upgrade their sidecars, which is an imperative operation, rather than a declarative one.

Additionally, emerging patterns from the field of Platform Engineering guide our understanding of enterprise roles.  In particular, two roles are represented in this Architecture: the Application Developer (AppDev) and Platform Engineer (PlatEng).

### Role: Application Developer

The AppDev is responsible for the development and delivery of their application, in this case the bookinfo e-commerce application.  The AppDev will make use of Istio features, such as HTTPRoutes, AuthorizationPolicy, and L4-L7 telemetry, but is not an expert in Istio, and should be unaware of installation and upgrades.  In most cases, AppDev's use of Istio APIs is both templated, to provide them with a simpler API surface, and limited by Policy with tools such as Gatekeeper.  Because the focus of this architecture, these technologies are not included here.

### Role: Platform Engineer

The PlatEng is responsible for providing the AppDev with a comprehensive application platform, which simplifies getting the application from Source Control to Production, as well as operating the App in Production.  As such, the PlatEng team must have a good deal of expertise in installing, operating, and automating a broad array of Cloud Native technologies, such as Service Mesh, Kubernetes, Observability stores and consumers, GitOps tooling, Policy enforcement, and templating tools such as Crossplane.  Due to this breadth, the Platform Engineer cannot spend all their time learning or operating any one technology, and any technology that is too difficult to operate is likely to be removed from the platform.

## Components

Istio is composed of six charts in Ambient Mode.  The components are divided between the Control Plane and the Data Plane, and some are Cluster-Scoped, while others can have multiple versions in a single cluster.

|                     | Control Plane              | Data Plane        |
| ------------------- |:--------------------------:| :----------------:|
| **Cluster-Scoped**  | CRDs + validation          | CNI<br>ztunnel    |
| **Workload-Scoped** | istiod<br>tags + revisions |  waypoint (envoy) |

Of these components, only waypoints (and other gateways) are intended to be operated by the AppDev (some users may choose to limit ingress gateways to the PlatEng role as well).  The remainder are the sole responsibility of the PlatEng role, and will be the focus of this reference architecture.

### Tags and Revisions

Istio components, particularly the waypoint, can specify which control plane they connect to (and by inference what version of the data plane they will run) using the `istio.io/rev` label set to a tag or revision.

As in sidecar mode, every control plane installation may (and should) include a revision name, which is a stable identifier for that control plane installation and version.  For simplicity, we recommend using the version of the control plane as the revision name (see [./istio/control-plane-appset.yaml:9](./istio/control-plane-appset.yaml), at .spec.generators[0].list.elements[*].revision).

Tags also identify control planes, but unlike revisions, tags are mutable references to revisions.  When an Istio Gateway (waypoint, ingress, or egress) references a particular tag, a dataplane is created using the version of the tag reference, and connects to the control plane indicated by the tag.  In this way, gateways can be organized into channels, or distinct groups which will be upgraded concurrently, without any involvement from the AppDev who owns the gateway.

In this reference architecture, three tags are used: stable, rapid, and default (the default tag will manage any gateways which do not use the `istio.io/rev` label).  In the example application, we have included an ingress gateway on the default tag, and two waypoints for the reviews and details services, which use the rapid and stable tags.  At the time of writing, the rapid revision points to revision 1-19-3, while the stable and default revisions point to revision 1-18-5.  The tags definitions can be found at [./istio/tags.yaml](./istio/tags.yaml).

## Upgrade Planning

This reference architecture provides the tools to declaratively manage your Istio Ambient installations with simple pull requests.  Before performing an upgrade, however, the PlatEng team should consider how they would like their upgrades to progress.  The two most common strategies are channels and phases, and these strategies can be combined.

In a phased model, there is generally a single version of Istio available in the cluster.  When a new version becomes available, the phases are moved one at a time to the new version, in order, until all phases have upgraded to the new model.  The phased model supports any number of phases based on the needs of your platform.

In a channel model, multiple versions of Istio are available for use by application developers at any point in time, based on their requirements for risk profile and new features or bugfixes.  For example, at the time of this writing, the stable tag or channel is using Istio 1.18.5, while the rapid channel is using Istio 1.19.3.  Under the channel model, these versions would be updated in-place as new patch releases are produced for various bugs or security concerns.  Then, when Istio 1.20.0 ships (ETA late November 2023), the rapid channel will be moved to point to version 1.20.0, while the stable version will be moved to point to 1.19.x (where x is the latest patch result at that time).  Because Istio releases are supported until two subsequent minor versions are shipiped (ie 1.18 will be supported until several weeks after 1.20 ships), this reference architecture uses only two channels, though more are possible.

![strategy diagram][strategies]

The channel and phased strategies can be combined into a comprehensive (though somewhat complicated) model where each channel contains phases, which determine the order of rollouts within the channel.

## Playbook: Minor Version Upgrade

COMING SOON

## Playbook: Major Version Upgrade

COMING SOON

## Tips and Tricks

For a quick (but messy) readout on what Istio versions are being used in this repo, run:

```bash
yq '.spec.generators[0].list.elements' < istio/control-plane-appset.yaml  && yq '.spec.source.helm.valuesObject.base.tags' < istio/tags.yaml && grep 'targetRevision' istio/*.yaml
```

[layout]: ./documentation/argo-reference-arch.svg "Repo Layout Diagram"
[strategies]: ./documentation/Ambient%20Upgrade%20-%20Strategies.png "Upgrade Strategies Diagram"
