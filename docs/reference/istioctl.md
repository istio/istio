---
title: istioctl
headline: 'The istioctl Command'
sidenav: doc-side-reference-nav.html
bodyclass: docs
layout: docs
type: markdown
---

`istioctl` is a command line interface for managing an Istio service mesh.  This overview covers
synax, describes command operations, and provides examples.

# Syntax

Istioctl commands follow the syntax:

```
istioctl <command> [targets] [flags]
```

where `command`, `targets` and `flags` are:

* **command**: the operation to perform, such as `create`, `delete`, `replace`, or `list`.
* **targets**: targets for commands such as delete
* **flags**: Optional flags.  For example specify `--file <filename>` to specify a configuration file to create from.

# Operations

* **create**: Create policies and rules
* **delete**: Delete policies or rules
* **get**: Retrieve policy/policies or rules
* **replace**: Replace policies and rules
* **version**: Display CLI version information

_kubernetes specific_
* **kube-inject**: Inject istio runtime proxy into kubernetes
  resources. This command has been added to aid in `istiofying`
  services for kubernetes and should eventually go away once a proper
  istio admission controller for kubernetes is available.

# Policy and Rule types

* **route-rule** Describes a rule for routing network traffic.  See [Route Rules](rule-dsl.md#route-rules) for details on routing rules.
* **destination-policy** Describes a policy for traffic destinations.  See [Destination Policies](rule-dsl.md#destination-policies) for details on destination policies.

# Examples of common operations

`istioctl create` - Create policies or rules from a file or stdin.

```
// Create a rule using the definition in example-routing.yaml.
$ istioctl create -f example-routing.yaml
```

`istioctl delete` - Create policies or rules from a file or stdin.

```
// Delete a rule using the definition in example-routing.yaml.
$ istioctl delete -f example-routing.yaml
```

`istioctl get` - List policies or rules in YAML format

```
// List route rules
istioctl get route-rules

// List destination policies
istioctl get destination-policies
```

# kube-inject

A short term workaround for the lack of a proper istio admision
controller is client-side injection. Use `istioctl kube-inject` to add the
necessary configurations to a kubernetes resource files.

    istioctl kube-inject -f deployment.yaml -o deployment-with-istio.yaml

Or update the resource on the fly before applying.

    kubectl create -f <(istioctl kube-inject -f depoyment.yaml)

Or update an existing deployment.

    kubectl get deployment -o yaml | istioctl kube-inject -f - | kubectl apply -f -

`istioctl kube-inject` will update
the [PodTemplateSpec](https://kubernetes.io/docs/api-reference/v1/definitions/#_v1_podtemplatespec) in
kubernetes Job, DaemonSet, ReplicaSet, and Deployment YAML resource
documents. Support for additional pod-based resource types can be
added as necessary.

Unsupported resources are left unmodified so, for example, it is safe
to run `istioctl kube-inject` over a single file that contains multiple
Service, ConfigMap, and Deployment definitions for a complex
application.

The Istio project is continually evolving so the low-level proxy
configuration may change unannounced. When in doubt re-run `istioctl kube-inject`
on your original deployments.
