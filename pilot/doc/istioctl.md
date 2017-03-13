# istioctl Overview

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
* **get**: Retrieve a policy or rule
* **list**: List policies and rules
* **replace**: Replace policies and rules
* **version**: Display CLI version information

# Policy and Rule types

* **route-rule** Describes a rule for routing network traffic.  See TODO for details on routing rules.
* **destination-policy** Describes a policy for traffic destinations.  See TODO for details on destination policies.

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

`istioctl list` - List policies or rules in YAML format

```
// List route rules
istioctl list route-rule

// List destination policies
istioctl list destination-policy
```
