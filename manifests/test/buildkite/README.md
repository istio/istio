# Experimental buildkite support for install-based tests

## Machine

install-machine.sh script has a basic install, i.e. agent plus tools used to run Istio tests (cached so we don't
download again).

## Kubernetes

Runs privileged, the container has access to the 'node' docker. Can run KIND, or could run tests
in a regular k8s container if we grant permissions to create pods.

## K8s - not privileged

A variant would be to run a non-priv agent, with namespace permissions.
The installer can add an option to not require cluster permissions, or only minimal cluster permissions granted
to the agent service account.

## Docker

Not clear if this is needed - but we can run the agent/builder inside a docker container. Since it has priv, no
major benefit compared with machine.
