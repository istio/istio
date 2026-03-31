# Istio Tags and Revisions

This document explains Istio's Tags and Revisions: their role in orchestration and xDS generation, and how users can create, update, and delete tags and revisions.

## Introduction

Istio uses the concepts of **Revisions** and **Tags** to manage multiple control plane versions and orchestrate progressive rollout, upgrades, and traffic control. These mechanisms allow users to run multiple Istio control planes in a cluster and direct workloads to specific versions or configurations.

## Conceptual Overview

- **Revision**: A unique identifier (e.g., `istio-system/revision-1-20-2`) for a specific Istio control plane deployment. Each revision manages its own set of webhooks, configuration, and xDS resources.
- **Tag**: An alias that points to a specific revision. Tags provide a stable reference for workloads and can be updated to point to new revisions without changing workload labels.
- Both tags and revisions influence which control plane instance orchestrates a workload and which xDS configuration it receives.

## Implementation Architecture

1. **Revisioned Control Planes**: Multiple Istiod deployments run in the cluster, each with a unique `--revision` flag.
2. **Webhook Selection**: Workloads are injected with sidecars by the webhook matching their `istio.io/rev` label (revision or tag).
3. **xDS Generation**: Each Istiod instance generates xDS resources only for workloads labeled with its revision or a tag pointing to it.
4. **Tag Management**: Tags are managed as custom resources and can be created, updated, or deleted to control traffic and upgrade flows.

### Key Relationships
- Workload labels (`istio.io/rev`) determine which revision or tag manages injection and xDS.
- Tags are mapped to revisions; updating a tag can shift many workloads to a new control plane version atomically.
- xDS resources are isolated per revision/tag, ensuring safe canary and progressive rollouts.

## Code Implementation

### Creating, Updating, and Deleting Tags and Revisions

#### 1. Creating a Revision
- Deploy Istiod with a unique revision:

```bash
istioctl install --set revision=canary
```
- This creates a new control plane and associated webhooks.

#### 2. Creating a Tag
- Create a tag pointing to a revision:

```bash
istioctl tag set prod --revision=canary
```
- This creates a `prod` tag that points to the `canary` revision.

#### 3. Updating a Tag
- Change the tag to point to a different revision:

```bash
istioctl tag set prod --revision=1-21-0
```
- All workloads labeled with `istio.io/rev=prod` will now be managed by the new revision.

#### 4. Deleting a Tag

```bash
istioctl tag remove prod
```
- Removes the tag and its associated webhook.

#### 5. Deleting a Revision
- Remove the Istiod deployment and associated resources for a revision:

```bash
istioctl uninstall --revision=canary
```

## Key Interfaces/Models

- Tag CRD: `manifests/charts/istio-control/istio-discovery/templates/revision-tags.yaml`
- Tag CLI: `istioctl/pkg/tag/tag.go`
- Revision detection: `pkg/revisions/tag_watcher.go`

## Example Use Cases

- **Canary Upgrade**: Deploy a new revision, create a tag, and gradually move workloads to the new version by updating the tag.
- **Blue/Green Deployment**: Use tags to switch traffic between two revisions with minimal disruption.
- **Rollback**: Quickly revert a tag to point to a previous revision if issues are detected.

## Best Practices

- Use tags for stable references in production; avoid labeling workloads directly with revision names unless necessary.
- Always test new revisions with a small set of workloads before updating tags cluster-wide.
- Clean up unused revisions and tags to avoid confusion and resource waste.
- Automate tag updates in CI/CD for safe, repeatable rollouts.

## Related Components

- [istioctl tag](istioctl_commands.md): CLI for managing tags
