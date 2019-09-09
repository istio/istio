# Istio.io Example Tests

## Overview
This folder contains tests for the different examples on Istio.io.

## Writing Tests


## Executing Tests Locally using Minikube
* Create a Minikube environment as advised on istio.io
* Run the tests as follows:
 go test ./... -p 1 --istio.test.env kube --istio.test.kube.config ~/.kube/config -v

