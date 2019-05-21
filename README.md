[![CircleCI](https://circleci.com/gh/istio/operator.svg?style=svg)](https://circleci.com/gh/istio/operator)
[![Mergify Status](https://gh.mergify.io/badges/istio/operator.png?style=cut)](https://mergify.io)
[![Go Report Card](https://goreportcard.com/badge/github.com/istio/operator)](https://goreportcard.com/report/github.com/istio/operator)
[![GolangCI](https://golangci.com/badges/github.com/istio/operator.svg)](https://golangci.com/r/github.com/istio/operator)

# Istio Operator

Update 4/22/19

This repo is for the initial, alpha version of the official Istio install operator. It's currently only open to the core 
design team for commits, while we merge prototypes that evolved in parallel. Once we have
a basic functional skeleton in place we'll open the repo up for community contributions.

The initial goals/MVP for the operator are:

1.   Replace helm tiller as the recommended install upgrade controller. We will continue to use helm templates and
the helm library to generate manifests under the hood, but will take ownership of the logic behind the install API.
The operator will have modes of operation analogous to tiller - controller in and out of cluster and one-shot manifest
generation for use with external toolchains.
1.   Provide a structured API to eventually replace the de-facto values.yaml API. This API will be partially moved
to CRDs used to configure Istio components directly, hence the operator API will only contain the portions that will 
remain in the operator CRD, and continue to use values.yaml for the parameters that will be moved to component CRDs.
The API will be aligned with Istio a-la-carte/building blocks philosophy and have a functional structure.
1.   Add an overlay mechanism to allow users to customize the rendered manifest in a way that survives upgrades. 
1.   Support upgrade, both in-place and dual control plane version rollover. The latter will be supported through the 
use of the [istio/installer](https://github.com/istio/installer) charts.
1.   Provide validation of all parameters.
