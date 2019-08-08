# Istio.io preliminary test cases

Welcome to the development of the istio.io preliminary test cases!

This work is experimental. This work is also under construction and may not work
for you. The Istio developers have done some work to document known problems or
workarounds. We expect this work will take several releases to complete.

## Overview
This folder contains tests for the different examples on [istio.io](https://istio.io).
The goal of this work is to ensure [istio.io](https://istio.io) is tested according
to the code in the [Istio repository](https://github.com/istio).

## Writing Tests

This section needs fleshing out.

## Executing Tests Locally using Minikube

- Create a [Minikube environment](https://preliminary.istio.io/docs/setup/kubernetes/platform-setup/minikube/)
  to develop the code.

There has been some indication that the default minikube environment values of 16GB
and 4 cores is not sufficient to run the test cases.  If after running the test cases,
you see istio-system pods in the pending state, please attempt to debug the reason
why they are pending so the Istio developers can improve these test cases.  Avaialbity
of running the test cases locally is crucial to the progression of this work.

- Change into the proper directory to run the test cases

```bash
$ cd $HOME/istio/pkg/test/istio.io
```

- Run the istio.io test cases:

```bash
$ go test ./... -p 1 --istio.test.env kube --istio.test.kube.config ~/.kube/config -v
```

You may see errors after running the test cases. This is expected, but not correct.  Please
try to debug the issues and either file an issue or a PR.
