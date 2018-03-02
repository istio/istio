# Developing for Istio

This document helps you get started developing code for Istio.
If you follow this guide and find some problem, please [submit an issue](https://github.com/istio/istio/issues/new),
so we can improve the document.
Also check [Troubleshooting](DEV-TROUBLESHOOTING.md).

- [Prerequisites](#prerequisites)
  - [Setting up Go](#setting-up-go)
  - [Dependency management](#setting-up-dep)
  - [Setting up Kubernetes](#setting-up-kubernetes)
    - [IBM Cloud Container Service](#ibm-cloud-container-service)
    - [Google Kubernetes Engine](#google-kubernetes-engine)
    - [Minikube](#minikube)
  - [Setting up environment variables](#setting-up-environment-variables)
  - [Setting up personal access token](#setting-up-a-personal-access-token)
- [Using the code base](#using-the-code-base)
  - [Building the code](#building-the-code)
  - [Building and pushing the containers](#building-and-pushing-the-containers)
  - [Building and pushing a specific container](#building-and-pushing-a-container)
  - [Building the Istio manifests](#building-the-istio-manifests)
  - [Cleaning outputs](#cleaning-outputs)
  - [Debug an Istio container with Delve](#debug-an-istio-container-with-delve)
  - [Running tests](#running-tests)
  - [Getting coverage numbers](#getting-coverage-numbers)
  - [Auto-formatting source code](#auto-formatting-source-code)
  - [Running the linters](#running-the-linters)
  - [Running race detection tests](#running-race-detection-tests)
  - [Adding dependencies](#adding-dependencies)
  - [About testing](#about-testing)
- [Working with CircleCI](#working-with-circleci)
- [Writing reference docs](#writing-reference-docs)
  - [About proto documentation](#about-proto-documentation)
- [Git workflow](#git-workflow)
  - [Fork the main repository](#fork-the-main-repository)
  - [Clone your fork](#clone-your-fork)
  - [Enable pre commit hook](#enable-pre-commit-hook)
  - [Create a branch and make changes](#create-a-branch-and-make-changes)
  - [Keeping your fork in sync](#keeping-your-fork-in-sync)
  - [Committing changes to your fork](#committing-changes-to-your-fork)
  - [Creating a pull request](#creating-a-pull-request)
  - [Getting a code review](#getting-a-code-review)
  - [When to retain commits and when to squash](#when-to-retain-commits-and-when-to-squash)

This document is intended to be relative to the branch in which it is found.
It is guaranteed that requirements will change over time for the development
branch, but release branches should not change.

## Prerequisites

Istio components only have few external dependencies you
need to set up before being able to build and run the code.

### Setting up Go

Many Istio components are written in the [Go](https://golang.org) programming language.
To build, you'll need a Go development environment. If you haven't set up a Go development
environment, please follow [these instructions](https://golang.org/doc/install)
to install the Go tools.

Istio currently builds with Go 1.9

### Setting up Docker

Istio has a Docker build system for creating and publishing Docker images.
To leverage that you will need:

- **Docker platform:** To download and install Docker follow [these instructions](https://docs.docker.com/install/).

- **Docker Hub ID:** If you do not yet have a Docker ID account you can follow [these steps](https://docs.docker.com/docker-id/) to create one. This ID will be used in a later step when setting up the environment variables.

### Setting up dep

Istio uses [dep](https://github.com/golang/dep) as the dependency
management tool for its Go codebase. Dep will be automatically installed as
part of the build. However, if you wish to install `dep` yourself, use the
following command:

```bash
go get -u github.com/golang/dep/cmd/dep
```

### Setting up Kubernetes

If you are working on Istio in a Kubernetes environment, we require
Kubernetes version 1.7.3 or higher. Follow the steps outlined in the
_prerequisites_ section in the
[Istio Quick Start](https://istio.io/docs/setup/kubernetes/quick-start.html)
to setup a Kubernetes cluster with Minikube, or launch a cluster in IBM
Cloud Container Service, Google Kubernetes Engine or OpenShift.

#### Additional steps for GKE

- Add `--no-enable-legacy-authorization` to the list of gcloud flags to fully
enable RBAC in GKE.

- Update your kubeconfig file with appropriate credentials to point kubectl
to the cluster created in GKE.

```shell
  gcloud container clusters get-credentials NAME --zone=ZONE
  ```

- Make sure you are using static client certificates before fetching cluster
credentials:

```shell
  gcloud config set container/use_client_certificate True
  ```

#### Additional notes for Minikube

Minikube version >= v0.22.3 is required for proper certificate
configuration for GenericAdmissionWebhook feature. Get the latest version
from
[minikube release page](https://github.com/kubernetes/minikube/releases)
for your platform.

```bash
minikube start \
    --extra-config=apiserver.Admission.PluginNames="Initializers,NamespaceLifecycle,LimitRanger,ServiceAccount,DefaultStorageClass,GenericAdmissionWebhook,ResourceQuota" \
    --kubernetes-version=v1.7.5
```

To enable RBAC, add `--bootstrapper kubeadm --extra-config=apiserver.Authorization.Mode=RBAC` to `minikube start` command, in addition to the flags above.

### Setting up environment variables

Set up your GOPATH, add a path entry for Go binaries to your PATH, set the ISTIO
path, and set your GITHUB_USER used later in this document. These exports are
typically added to your ~/.profile:

```shell
export GOPATH=~/go
export PATH=$PATH:$GOPATH/bin
export ISTIO=$GOPATH/src/istio.io # eg. ~/go/src/istio.io

# Please change HUB to the desired HUB for custom docker container
# builds.
export HUB="docker.io/$USER"

# The Istio Docker build system will build images with a tag composed of
# $USER and timestamp. The codebase doesn't consistently use the same timestamp
# tag. To simplify development the development process when later using
# updateVersion.sh you may find it helpful to set TAG to something consistent
# such as $USER.
export TAG=$USER

# If your github username is not the same as your local user name (saved in the
# shell variable $USER), then replace "$USER" below with your github username
export GITHUB_USER=$USER

# Specify which Kube config you'll use for testing. This depends on whether
# you're using Minikube or your own Kubernetes cluster for local testing
# For a GKE cluster:
export KUBECONFIG=${HOME}/.kube/config
# Alternatively, for Minikube:
# export KUBECONFIG=${GOPATH}/src/istio.io/istio/.circleci/config

```

Execute once, to create a directory for the Istio source trees.

```shell
mkdir -p $ISTIO
```

As the steps recommended in this section change the $PATH and environment,
you will need to reload the environment.

### Setting up a personal access token

This is only necessary for core contributors in order to push changes to the main repos.
You can make pull requests without two-factor authentication
but the additional security is recommended for everyone.

To be part of the Istio organization, we require two-factor authentication, and
you must setup a personal access token to enable push via HTTPS. Please follow
[these instructions](https://help.github.com/articles/creating-a-personal-access-token-for-the-command-line/)
for how to create a token.
Alternatively you can [add your SSH keys](https://help.github.com/articles/adding-a-new-ssh-key-to-your-github-account/).

## Using the code base

### Building the code

To build Pilot, Mixer, and Istio CA for your host architecture, run

```shell
make
```

This build command figures out what it needs to do and does not need any
input from you.

To build those components with debugger information so that a debugger such as
[Delve](https://github.com/derekparker/delve) can be used to debug them, run


```shell
make DEBUG=1
```

*TIP*: To speed up consecutive builds of the project, run the following
command instead:

```shell
GOBUILDFLAGS=-i make
```

`GOBUILDFLAGS=-i` causes our build system to build with `go build -i`, that
results in significant improvements of the overall build time. Note that
the use of `-i` flag causes Go to cache intermediate results in
`$GOPATH/pkg/`. Depending on the situation, this behaviour may be
undesirable as Golang may not erase out of date artifacts from the
cache. In such a situation, erase the contents of `$GOPATH/pkg/` manually
before rebuilding the code.

### Building and pushing the containers

Build the containers in your local docker cache:

```shell
make docker
```

To build the containers with the debugger information so that they can be
debugged with a debugger such as [Delve](https://github.com/derekparker), run

```shell
make DEBUG=1 docker
```

Push the containers to your registry:

```shell
make push
```

### Building and pushing a specific container.

If you want to make a local change and test some component, say istio-ca, you
could do:

Under istio/istio repo

```shell
pwd
```
The path should be

```shell
.../src/istio.io/istio
```

Set up environment variables HUB and TAG by
```shell
export HUB=docker.io/yourrepo
export TAG=istio-ca
```

Make some local change of CA code, then build istio-ca

```shell
bin/gobuild.sh istio_ca istio.io/istio/pkg/version ./security/cmd/istio_ca
```

Note: for other images, check Makefile for more info.

And move this file to docker_temp repo
```shell
cp istio_ca $GOPATH/out/linux_amd64/release/docker_temp
```

Push docker image
```shell
make push.docker.istio-ca
```

### Building the Istio manifests

Use [updateVersion.sh](https://github.com/istio/istio/blob/master/install/updateVersion.sh)
to generate new manifests with mixer, pilot, and ca_cert custom built containers:

```shell
install/updateVersion.sh -a ${HUB},${TAG}
```

### Cleaning outputs

You can delete any build artifacts with:

```shell
make clean
```

### Debug an Istio container with Delve

To debug an Istio container with Delve in a Kubernetes environment:

* Locate the Kubernetes node on which your container is running.
* Make sure that the node has Go tool installed as described in above.
* Make sure the node has [Delve installed](https://github.com/derekparker/delve/tree/master/Documentation/installation).
* Clone the Istio repo from which your debuggable executables
   have been built onto the node.
* Log on to the node and find out the process id that you'd like to debug. For
   example, if you want to debug Pilot, the process name is pilot-discovery.
   Issue command ```ps -ef | grep pilot-discovery``` to find the process id.
* Issue the command ```sudo dlv attach <pilot-pid>``` to start the debug
   session.

You may find this [Delve tutorial](http://blog.ralch.com/tutorial/golang-debug-with-delve/) is useful.

Alternatively, you can use [Squash](https://github.com/solo-io/squash) with
Delve to debug your container. You may need to modify the Istio Dockerfile to
use a base image such as alpine (versus scratch in Pilot Dockerfiles). One of
the benefits of using Squash is that you don't need to install Go tool and Delve
on every Kubernetes nodes.

### Running tests

You can run all the available tests with:

```shell
make test
```

*Note on Pilot unit tests:* For tests that require systems integration,
such as invoking the Envoy proxy with a special configuration, we capture
the desired output as golden artifacts and save the artifacts in the
repository. Validation tests compare generated output against the desired
output. For example,
[Envoy configuration test data](pilot/pkg/proxy/envoy/testdata) contains
auto-generated proxy configuration. If you make changes to the configuration
generation, you also need to create or update the golden artifact in the
same pull request. The test library can automatically refresh all golden
artifacts if you pass a special environment variable:

```bash
env REFRESH_GOLDEN=true make pilot-test
```

### Getting coverage numbers

You can get the current unit test coverage numbers on your local repo by going to the top of the repo and entering:

```shell
make coverage
```

### Auto-formatting source code

You can automatically format the source code to follow our conventions by going to the
top of the repo and entering:

```shell
make format
```

### Running the linters

You can run all the linters we require on your local repo by going to the top of the repo and entering:

```shell
make lint
# To run only on your local changes
bin/linters.sh -s HEAD^
```

### Race detection tests

You can run the test suite using the Go race detection tools using:

```shell
make racetest
```

### Adding dependencies

It will occasionally be necessary to add a new external dependency to the
system. If the dependent Go package does not have to be pinned to a
specific version, run `dep ensure` to update the Gopkg.lock files and
commit them along with your code. If the dependency has to be pinned to a
specific version, run

```bash
dep ensure -add github.com/foo/bar
```

The command above adds a version constraint to Gopkg.toml and updates
Gopkg.lock. Inspect Gopkg.toml to ensure that the package is pinned to the
correct SHA. _Please pin to COMMIT SHAs instead of branches or tags._

You will need to commit the vendor/ change through a separate PR, please see
https://github.com/istio/istio/wiki/Vendor-FAQ#how-do-i-add--change-a-dependency

### About testing

Before sending pull requests you should at least make sure your changes have
passed both unit and integration tests. We only merge pull requests when
**all** tests are passing.

- Unit tests should be fully hermetic
  - Only access resources in the test binary.
- All packages and any significant files require unit tests.
- Unit tests are written using the standard Go testing package.
- The preferred method of testing multiple scenarios or input is
  [table driven testing](https://github.com/golang/go/wiki/TableDrivenTests)
- Concurrent unit test runs must pass.

## Working with CircleCI

We use CircleCI as one of the systems for continuous integration. Any PR
will have to pass all CircleCI tests (in addition to Prow tests) before
being ready to merge. When you fork the Istio repository, you will
automatically inherit the CircleCI testing environment as well, allowing
you to fully reproduce our testing infrastructure. If you have already
signed up for CircleCI, you can test your code changes in your fork against
the full suite of tests that we run for every PR.

Please refer to the
[wiki](https://github.com/istio/istio/wiki/Working-with-CircleCI) for a
detailed guide on using CircleCI with Istio.

## Writing reference docs

Our users depend on having quality documentation for our product. In addition to the prose
you should be creating in the `istio/istio.github.io` repo, you need to also write quality reference documentation
throughout the product.

Our reference documentation comes from two places:

- Comments on proto files for our config formats and API definitions. The various
protos in the `istio/api` repo, the Mixer adapter configuration protos, and the Mixer
template definitions are all examples of this.

- Cobra commands for our CLI docs. The various CLI commands we build generally use the
Cobra framework to parse our command-lines. Cobra is also used to produce
reference documentation for the individual commands.

Here's how this works:

- Developers comment every element in proto files they author

- Developers provide quality help & usage text in Cobra command definitions.

- Whenever we compile protos, in addition to producing `.pb.go` files, our build system
also produces `.pb.html` files which hold the documentation for the particular proto package.

- Within the `istio/istio.github.io` repo, the script file
[scripts/grab_reference_docs.sh](https://github.com/istio/istio.github.io/blob/master/scripts/grab_reference_docs.sh)
does the work necessary to harvest all the generated `.pb.html` files from the `istio/api`
and `istio/istio` repos and installs them in the right place in the website hierarchy.
The script also builds and runs the various CLI commands we have in order to extract their
documentation, and copies their docs to the right place in the website hierarchy. Once the
script is done running, the changes to the website just need to be pushed and istio.io will
shortly thereafter reflect the changes.

### About proto documentation

Writing proto documentation is a simple matter of adding comments to elements
within the input proto files. You can put comments directly above individual elements, or to the
right. For example:

```proto
// A package-level comment
package pkg;

// This documents the message as a whole
message MyMsg {
    // This documents this field
    // It can contain many lines.
    int32 field1 = 1;

    int32 field2 = 2;       // This documents field2
}
```

Comments are treated as markdown. You can thus embed classic markdown annotations within any comment.

#### Linking to types and elements

In addition to normal markdown links, you can also use special proto links within any comment. Proto
links are used to create a link to other types or elements within the set of protos. You specify proto links
using two pairs of square brackets such as:

```proto
// This is a comment that links to another type: [MyOtherType][MyPkg.MyOtherType]
message MyMsg {

}

```

The first square brackets contain the name of the type to display in the resulting documentation. The second
square brackets contain the fully qualified name of the type or element being referenced, including the
package name.

#### Annotations

Within a proto file, you should insert special comments which provide additional metadata to
use in producing quality documentation. Within a package, include an unattached
comment of the form:

```
// $title: My Title
// $overview: My Overview
// $location: https://mysite.com/mypage.html
```

`$title` provides a title for the generated package documentation. This is used for things like the
title of the generated HTML. `$overview` is a one-line description of the package, useful for
tables of contents or indexes. Finally, `$location` indicates the expected URL for the generated
documentation. This is used to help downstream processing tools to know where to copy
the documentation, and is used when creating documentation links from other packages to this one.

You can also use the $front_matter annotation to introduce new Jekyll front matter which can be useful
for special tuning of the output. For example:

```
// $front_matter: order: 10
```

The above will include the front matter `order: 10` in the generated Jekyll HTML document.

If a comment for an element contains the annotation `$hide_from_docs`,
then the associated element will be omitted from the output. This is useful when staging the
introduction of new features that aren't quite ready for use yet. The annotation can appear
anywhere in the comment for the element. For example:

```proto
message MyMsg {
    int32 field1 = 1; // $hide_from_docs
}
```

The comment for any element can contain the annotation `$class: <foo>` which is used
to insert a specific HTML class around the generated element. This is useful to give
particular styling to particular elements. Common examples of useful classes include

```proto
message MyMsg {
    int32 field1 = 1; // $class: alpha
    int32 field2 = 2; // $class: beta
    int32 field3 = 3; // $class: experimental
}
```

The specific class used can be used to control the rendering of the element. At the moment, Istio
doesn't provide any specific rendering controls for these classes, but it would be easy to add some.
As shown in the example, we could readily have different rendering for alpha elements vs. beta vs.
experimental. If you find this would be a useful feature, file an issue in `istio/istio.github.io`
to introduce the CSS necessary to support the particular class you want to use.

#### Proto doc checklist

- Ensure each package contains the appropriate `$title`, `$overview`, and `$location` annotations such
that your package's docs are properly published to istio.io.

- Ensure each package contains exactly one package-level comment. This comment is displayed at the top of the
documentation page.

- Ensure each element (message, enum, message field, enum value, etc) has good operator-centric documentation.

## Git workflow

Below, we outline one of the more common Git workflows that core developers use.
Other Git workflows are also valid.

### Fork the main repository

1. Go to https://github.com/istio/istio
1. Click the "Fork" button (at the top right)

### Clone your fork

The commands below require that you have $GOPATH set ([$GOPATH
docs](https://golang.org/doc/code.html#GOPATH)). We highly recommend you put
Istio's code into your GOPATH. Note: the commands below will not work if
there is more than one directory in your `$GOPATH`.

```shell
cd $ISTIO
git clone https://github.com/$GITHUB_USER/istio
cd istio
git remote add upstream 'https://github.com/istio/istio'
git config --global --add http.followRedirects 1
```

### Enable pre-commit hook

NOTE: The precommit hook is not functional as of 11/08/2017 following the repo
reorganization. It should come back alive shortly.

Istio uses a local pre-commit hook to ensure that the code
passes local tests before being committed.

Run

```shell
./bin/pre-commit
Installing pre-commit hook
```

This hook is invoked every time you commit changes locally.
The commit is allowed to proceed only if the hook succeeds.

### Create a branch and make changes

```shell
git checkout -b my-feature
# Make your code changes
```

### Keeping your fork in sync

```shell
git fetch upstream
git rebase upstream/master
```

Note: If you have write access to the main repositories
(e.g. github.com/istio/istio), you should modify your Git configuration so
that you can't accidentally push to upstream:

```shell
git remote set-url --push upstream no_push
```

### Committing changes to your fork

When you're happy with some changes, you can commit them to your repo:

```shell
git add .
git commit
```

Then push the change to your fork (typically called `origin`). When prompted for authentication, use your
GitHub username as usual but the personal access token as your password if you
have not setup ssh keys. Please
follow [these instructions](https://help.github.com/articles/caching-your-github-password-in-git/#platform-linux)
if you want to cache the token.

```shell
git push origin my-feature
```

### Creating a pull request

1. Visit https://github.com/$GITHUB_USER/istio if you created a fork in your own github repository, or https://github.com/istio/istio and navigate to your branch (e.g. "my-feature").
1. Click the "Compare" button to compare the change, and then the "Pull request" button next to your "my-feature" branch.

### Getting a code review

Once your pull request has been opened it will be assigned to one or more
reviewers. Those reviewers will do a thorough code review, looking for
correctness, bugs, opportunities for improvement, documentation and comments,
and style.

Very small PRs are easy to review. Very large PRs are very difficult to
review. GitHub has a built-in code review tool, which is what most people use.

### When to retain commits and when to squash

Upon merge, all Git commits should represent meaningful milestones or units of
work. Use commits to add clarity to the development and review process.

Before merging a PR, squash any "fix review feedback", "typo", and "rebased"
sorts of commits. It is not imperative that every commit in a PR compile and
pass tests independently, but it is worth striving for. For mass automated
fixups (e.g. automated doc formatting), use one or more commits for the
changes to tooling and a final commit to apply the fixup en masse. This makes
reviews much easier.

Please do not use force push after submitting a PR to resolve review
feedback. Doing so results in an inability to see what has changed between
revisions of the PR. Instead submit additional commits until the PR is
suitable for merging. Once the PR is suitable for merging, the commits will
be squashed to simplify the commit.
