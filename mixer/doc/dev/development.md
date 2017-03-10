# Development Guide

This document is intended to be the canonical source of truth for things like
supported toolchain versions for building the Istio mixer. If you find a
requirement that this doc does not capture, or if you find other docs with
references to requirements that are not simply links to this doc, please
[submit an issue](https://github.com/istio/mixer/issues/new).

This document is intended to be relative to the branch in which it is found.
It is guaranteed that requirements will change over time for the development
branch, but release branches of the Istio mixer should not change.

- [Prerequisites](#prerequisites)
  - [Setting up Go](#setting-up-go)
  - [Setting up Bazel](#setting-up-bazel)
  - [Setting up Docker](#setting-up-docker)
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
  - [Cleaning outputs](#cleaning-outputs)
  - [Running tests](#running-tests)
  - [Getting coverage numbers](#getting-coverage-numbers)
  - [Auto-formatting source code](#auto-formatting-source-code)
  - [Running the linters](#running-the-linters)
  - [Running race detection tests](#running-race-detection-tests)
  - [Adding dependencies](#adding-dependencies)
  - [About testing](#about-testing)
- [Using the mixer](#using-the-mixer)

Other docs you should look at:

- [Project conventions](./conventions.md)
- [Creating fast and lean code](./performance.md)
- [Writing mixer adapters](./adapters.md)
- [Go landmines](https://gist.github.com/lavalamp/4bd23295a9f32706a48f)
- [Go style mistakes](https://github.com/golang/go/wiki/CodeReviewComments)

## Prerequisites

The mixer code base has only a few external dependencies you
need to setup before being able to build and run the code.

### Setting up Go

The Istio mixer is written in the [Go](http://golang.org) programming language.
To build the mixer, you'll need a Go development environment. Builds for
the mixer require Go version 1.7. If you haven't set up a Go development
environment, please follow [these instructions](http://golang.org/doc/code.html)
to install the Go tools.

Set up your GOPATH and add a path entry for Go binaries to your PATH. Typically
added to your ~/.profile:

```
export GOPATH=~/go
export PATH=$PATH:$GOPATH/bin
```

### Setting up Bazel

The Istio mixer is built using the bazel build system. See
[here](https://bazel.build/versions/master/docs/install.html) for the
installation procedures.

### Setting up Docker

To run some of the mixer examples and tests, you need to set up Docker server.
Please follow [these instructions](https://docs.docker.com/engine/installation/)
for how to do this for your platform.

## Git workflow

Below, we outline one of the more common git workflows that core developers use.
Other git workflows are also valid.

### Fork the main repository

1. Go to https://github.com/istio/mixer
2. Click the "Fork" button (at the top right)

### Clone your fork

The commands below require that you have $GOPATH set ([$GOPATH
docs](https://golang.org/doc/code.html#GOPATH)). We highly recommend you put
the mixer's code into your GOPATH. Note: the commands below will not work if
there is more than one directory in your `$GOPATH`.

```
export ISTIO=~/go/src/istio.io
mkdir -p $(ISTIO)/mixer
cd $ISTIO

# Replace "$YOUR_GITHUB_USERNAME" below with your github username
git clone https://github.com/$YOUR_GITHUB_USERNAME/mixer.git
cd mixer
git remote add upstream 'https://github.com/istio/mixer.git'
git config --global --add http.followRedirects 1
```
### Enable pre-commit hook

Mixer uses a local pre-commit hook to ensure that the code
passes local test.

Run
```
user@host:~/GOHOME/src/istio.io/mixer$ bin/pre-commit
Installing pre-commit hook
```
This hook is invoked every time you commit changes locally.
The commit is allowed to proceed only if the hook succeeds.

### Create a branch and make changes

```
git checkout -b my-feature
# Make your code changes
```

### Keeping your fork in sync

```
git fetch upstream
git rebase upstream/master
```

Note: If you have write access to the main repository at
github.com/istio/mixer, you should modify your git configuration so
that you can't accidentally push to upstream:

```
git remote set-url --push upstream no_push
```

### Committing changes to your fork

When you're happy with some changes, you can commit them and push them to your fork:

```
git add .
git commit
git push -f origin my-feature
```

### Creating a pull request

1. Visit https://github.com/$YOUR_GITHUB_USERNAME/mixer
2. Click the "Compare & pull request" button next to your "my-feature" branch.

### Getting a code review

Once your pull request has been opened it will be assigned to one or more
reviewers. Those reviewers will do a thorough code review, looking for
correctness, bugs, opportunities for improvement, documentation and comments,
and style.

Very small PRs are easy to review.  Very large PRs are very difficult to
review. Github has a built-in code review tool, which is what most people use.

### When to retain commits and when to squash

Upon merge, all git commits should represent meaningful milestones or units of
work. Use commits to add clarity to the development and review process.

Before merging a PR, squash any "fix review feedback", "typo", and "rebased"
sorts of commits. It is not imperative that every commit in a PR compile and
pass tests independently, but it is worth striving for. For mass automated
fixups (e.g. automated doc formatting), use one or more commits for the
changes to tooling and a final commit to apply the fixup en masse. This makes
reviews much easier.

## Using the code base

### Building the code

To build the mixer, enter:

```
cd $(ISTIO)/mixer
bazel build ...
```

This figures out what it needs to do and does not need any input from you.

### Cleaning outputs

You can delete any build artifacts with:

```
bazel clean
```
### Running tests

You can run all the available tests with:

```
bazel test ...
```
### Getting coverage numbers

You can get the current unit test coverage numbers on your local repo by going to the top of the repo and entering:

```
make coverage
```

### Auto-formatting source code

You can automatically format the source code and BUILD files to follow our conventions by going to the
top of the repo and entering:

```
make fmt
```

### Running the linters

You can run all the linters we require on your local repo by going to the top of the repo and entering:

```
make lint
```

### Race detection tests

You can run the test suite using the Go race detection tools using:

```
make racetest
```

### Adding dependencies

It will occasionally be necessary to add a new dependency to the Istio Mixer, 
either in support of a new adapter or to provide additional core functionality. 

Mixer dependencies are maintained in the [WORKSPACE](https://github.com/istio/mixer/blob/master/WORKSPACE)
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

```
new_go_repository(
    name = "org_golang_google_grpc",
    commit = "708a7f9f3283aa2d4f6132d287d78683babe55c8", # Dec 5, 2016 (v1.0.5)
    importpath = "google.golang.org/grpc",
)
```

```
git_repository(
    name = "org_pubref_rules_protobuf",
    commit = "b0acb9ecaba79716a36fdadc0bcc47dedf6b711a", # Nov 28 2016 (importmap support for gogo_proto_library)
    remote = "https://github.com/pubref/rules_protobuf",
)
```


### About testing

Before sending pull requests you should at least make sure your changes have
passed both unit and integration tests. We only merges pull requests when
**all** tests are passing.

* Unit tests should be fully hermetic
  - Only access resources in the test binary.
* All packages and any significant files require unit tests.
* The preferred method of testing multiple scenarios or input is
  [table driven testing](https://github.com/golang/go/wiki/TableDrivenTests)
* Concurrent unit test runs must pass.

## Using the mixer

Once you've built the source base, you can run the mixer in a basic mode using:

```
bazel-bin/cmd/server/mixs server
  --globalConfigFile testdata/globalconfig.yml
  --serviceConfigFile testdata/serviceconfig.yml  --logtostderr
```

You can also run a simple client to interact with the server:

```
bazel-bin/cmd/client/mixc check
```
