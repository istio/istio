# Developing for Istio

This document helps you get started to develop code for Istio.
If you're following this guide and find some problem, please [submit an issue](https://github.com/istio/istio/issues/new).
so we can improve the doc.

- [Prerequisites](#prerequisites)
  - [Setting up Go](#setting-up-go)
  - [Setting up Bazel](#setting-up-bazel)
  - [Setting up Docker](#setting-up-docker)
  - [Setting up environment variables](#setting-up-environment-variables)
  - [Setting up personal access token](#setting-up-a-personal-access-token)
  - [Setting up a container registry](#setting-up-a-container-registry)
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
- [Using the code base](#using-the-code-base)
  - [Building the code](#building-the-code)
  - [Building the containers](#building-the-containers)
  - [Building the Istio manifests](#building-the-istio-manifests)
  - [Cleaning outputs](#cleaning-outputs)
  - [Running tests](#running-tests)
  - [Getting coverage numbers](#getting-coverage-numbers)
  - [Auto-formatting source code](#auto-formatting-source-code)
  - [Running the linters](#running-the-linters)
  - [Running race detection tests](#running-race-detection-tests)
  - [Adding dependencies](#adding-dependencies)
  - [About testing](#about-testing)
- [Local development scripts](#collection-of-scripts-and-notes-for-developing-for-istio)

This document is intended to be relative to the branch in which it is found.
It is guaranteed that requirements will change over time for the development
branch, but release branches should not change.

## Prerequisites

Istio components only have few external dependencies you
need to setup before being able to build and run the code.

### Setting up Go

Many Istio components are written in the [Go](http://golang.org) programming language.
To build, you'll need a Go development environment. If you haven't set up a Go development
environment, please follow [these instructions](https://golang.org/doc/install)
to install the Go tools.

Istio currently builds with Go 1.9

### Setting up Bazel

Istio components are built using the Bazel build system. See
[here](https://bazel.build/versions/master/docs/install.html) for the
installation procedures.
In addition to Bazel itself, you should install the Bazel buildifier tool from
[here](https://github.com/bazelbuild/buildtools).

Istio currently builds with Bazel 0.7.0

### Setting up Docker

To run some of Istio's examples and tests, you need to set up Docker server.
Please follow [these instructions](https://docs.docker.com/engine/installation/)
for how to do this for your platform.

### Setting up environment variables

Set up your GOPATH, add a path entry for Go binaries to your PATH, set the ISTIO
path, and set your GITHUB_USER used later in this document. These exports are
typically added to your ~/.profile:

```shell
export GOPATH=~/go
export PATH=$PATH:$GOPATH/bin
export ISTIO=$GOPATH/src/istio.io # eg. ~/go/src/istio.io

# If your github username is not the same as your local user name (saved in the
# shell variable $USER), then replace "$USER" below with your github username
export GITHUB_USER=$USER
```

Execute a one time operation to contain the Istio source trees.

```shell
mkdir -p $ISTIO
```

### Setting up a personal access token

This is only necessary for core contributors in order to push changes to the main repos.
You can make pull requests without two-factor authentication
but the additional security is recommended for everyone.

To be part of the Istio organization, we require two-factor authentication, and
you must setup a personal access token to enable push via HTTPS. Please follow
[these instructions](https://help.github.com/articles/creating-a-personal-access-token-for-the-command-line/)
for how to create a token.
Alternatively you can [add your SSH keys](https://help.github.com/articles/adding-a-new-ssh-key-to-your-github-account/).

### Setting up a container registry

Follow the
[Google Container Registry Quickstart](https://cloud.google.com/container-registry/docs/quickstart).

## Git workflow

Below, we outline one of the more common Git workflows that core developers use.
Other Git workflows are also valid.

### Fork the main repository

1. Go to https://github.com/istio/istio
2. Click the "Fork" button (at the top right)

### Clone your fork

The commands below require that you have $GOPATH set ([$GOPATH
docs](https://golang.org/doc/code.html#GOPATH)). We highly recommend you put
Istio's code into your GOPATH. Note: the commands below will not work if
there is more than one directory in your `$GOPATH`.

```shell
cd $ISTIO
git clone https://github.com/$GITHUB_USER/istio.git
cd istio
git remote add upstream 'https://github.com/istio/istio.git'
git config --global --add http.followRedirects 1
```

### Enable pre-commit hook

NOTE: The precommit hook is not functional as of 11/08/2017 following the repo
reorganization. It should come back alive shortly.

Istio uses a local pre-commit hook to ensure that the code
passes local tests before being committed.

Run
```shell
user@host:~/GOHOME/src/istio.io/istio$ ./bin/pre-commit
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
Then push the change to the fork. When prompted for authentication, use your
GitHub username as usual but the personal access token as your password if you
have not setup ssh keys. Please
follow [these instructions](https://help.github.com/articles/caching-your-github-password-in-git/#platform-linux)
if you want to cache the token.

```shell
git push -f origin my-feature
```

### Creating a pull request

1. Visit https://github.com/$GITHUB_USER/istio
2. Click the "Compare & pull request" button next to your "my-feature" branch.

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

## Using the code base

### Building the code

To build the core repo:

```shell
cd $ISTIO/istio
make build
```

This build command figures out what it needs to do and does not need any input from you.

### Setup bazel and go links

Symlinks bazel artifacts into the standard go structure so standard go
tooling functions correctly

```shell
./bin/bazel_to_go.py
```
(You can safely ignore some errors like
`com_github_opencontainers_go_digest Does not exist`)

### Building the containers

This tool builds and publishes Mixer container images to the specified
registry.

```
bin/publish-docker-images.sh -h gcr.io/my-project -t my-tag
```

where

* The `-h` parameter `gcr.io/my-project` is the composition of the registry
  hostname and the project id. This should be customized.
* The `-t` parameter `my-tag` is the desired tag. This should be customized.

### Building the Istio manfiests

Use [updateVersion.sh](https://github.com/istio/istio/blob/master/install/updateVersion.sh)
to generate new manifests with the specified Mixer containers.

```
cd $ISTIO/istio
install/updateVersion.sh -xgcr.io/my-project,my-tag
```

where

* `gcr.io/my-project` is equivalent to the `-h` parameter specified to
  `publish-docker-images.sh`.
* `my-tag` is equivalent to the `-t` parameter specified to
  `publish-docker-images.sh`.
* `-x` and `,` and the parameters are not delimited by a space.

### Cleaning outputs

You can delete any build artifacts with:

```shell
make clean
```
### Running tests

You can run all the available tests with:

```shell
make test
```
### Getting coverage numbers

You can get the current unit test coverage numbers on your local repo by going to the top of the repo and entering:

```shell
make coverage
```

### Auto-formatting source code

You can automatically format the source code and BUILD files to follow our conventions by going to the
top of the repo and entering:

```shell
make fmt
```

### Running the linters

You can run all the linters we require on your local repo by going to the top of the repo and entering:

```shell
make lint
# To run only on your local changes
bin/linters.sh -s HEAD^
```

### Source file dependencies

You can keep track of dependencies between sources using:

```shell
make gazelle
```

### Race detection tests

You can run the test suite using the Go race detection tools using:

```shell
make racetest
```

### Adding dependencies

It will occasionally be necessary to add a new external dependency to the system
Dependencies are maintained in the [WORKSPACE](https://github.com/istio/istio/blob/master/WORKSPACE)
file. To add a new dependency, please append to the bottom on the file. A dependency
can be added manually, or via [wtool](https://github.com/bazelbuild/rules_go/blob/master/go/tools/wtool/main.go).

All dependencies:
- *MUST* be specified in terms of commit SHA (vs release tag).
- *MUST* be annotated with the commit date and an explanation for the choice of
commit. Annotations *MUST* follow the `commit` param as a comment field.
- *SHOULD* be targeted at a commit that corresponds to a stable release of the
library. If the library does not provide regular releases, etc., pulling from a
known good recent commit is acceptable.

Examples:

```shell
new_go_repository(
    name = "org_golang_google_grpc",
    commit = "708a7f9f3283aa2d4f6132d287d78683babe55c8", # Dec 5, 2016 (v1.0.5)
    importpath = "google.golang.org/grpc",
)
```

```shell
git_repository(
    name = "org_pubref_rules_protobuf",
    commit = "b0acb9ecaba79716a36fdadc0bcc47dedf6b711a", # Nov 28 2016 (importmap support for gogo_proto_library)
    remote = "https://github.com/pubref/rules_protobuf",
)
```


### About testing

Before sending pull requests you should at least make sure your changes have
passed both unit and integration tests. We only merge pull requests when
**all** tests are passing.

* Unit tests should be fully hermetic
  - Only access resources in the test binary.
* All packages and any significant files require unit tests.
* The preferred method of testing multiple scenarios or input is
  [table driven testing](https://github.com/golang/go/wiki/TableDrivenTests)
* Concurrent unit test runs must pass.


## Collection of scripts and notes for developing Istio

For local development (building from source and running the major components) on Ubuntu/raw VM:

Assuming you did (once):
1. [Install bazel](https://bazel.build/versions/master/docs/install-ubuntu.html), note that as of this writing Bazel needs the `openjdk-8-jdk` VM (you might need to uninstall or get out of the way the `ibm-java80-jdk` that comes by default with GCE for instance)
2. Install required packages: `sudo apt-get install make openjdk-8-jdk libtool m4 autoconf uuid-dev cmake golang-go`
3. Get the source trees
   ```bash
   mkdir github
   cd github/
   git clone https://github.com/istio/istio.git
   ```
4. You can then use
   - [update_all](update_all) : script to build from source
   - [setup_run](setup_run) : run locally
   - [fortio](https://github.com/istio/fortio/) (φορτίο) : load testing and minimal echo http and grpc server
   - And an unrelated tool to aggregate [GitHub Contributions](githubContrib/) statistics.
5. And run things like
   ```bash
   # Test the echo server:
   curl -v http://localhost:8080/
   # Test through the proxy:
   curl -v http://localhost:9090/echo
   # Add a rule locally (simply drop the file or exercise the API:)
   curl -v  http://localhost:9094/api/v1/scopes/global/subjects/foo.svc.cluster.local/rules --data-binary @quota.yaml -X PUT -H "Content-Type: application/yaml"
   # Test under some load:
   fortio load -qps 2000 http://localhost:9090/echo

   ```
   Note that this is done for you by [setup_run](setup_run) but to use the correct go environment:
   ```bash
   cd mixer/
   source bin/use_bazel_go.sh
   ```
