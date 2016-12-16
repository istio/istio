# Istio APIs and Common Configuration Definitions

This repo defines component-level APIs and common configuration formats for the Istio
platform. These definitions are specified using the [protobuf](https://github.com/google/protobuf)
syntax.

All other Istio repositories can take a dependency on the api
repository. This repository *will not* depend on any other repos

We may check-in generated .pb.go and .pb.cc files here.

## Standard vocabulary

All components of an Istio installation operate on a shared vocabulary of attributes.
A standard vocabulary of attributes including it meaning is available in this repo.
