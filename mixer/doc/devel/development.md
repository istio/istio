# Development Guide

This document is intended to be the canonical source of truth for things like
supported toolchain versions for building the Istio mixer. If you find a
requirement that this doc does not capture, please
[submit an issue](https://github.com/istio/mixer/issues/new) on github. If
you find other docs with references to requirements that are not simply links to
this doc, please [submit an issue](https://github.com/istio/mixer/issues/new).

This document is intended to be relative to the branch in which it is found.
It is guaranteed that requirements will change over time for the development
branch, but release branches of the Istio mixer should not change.

- [Prerequisites](#prerequisites)
  - [Setting up Go](#setting-up-go)
  - [Setting up Docker](#setting-up-docker)
- [Git workflow](#git-workflow)
  - [Fork the main repository](#fork-the-main-repository)
  - [Clone your fork](#clone-your-fork)
  - [Create a branch and make changes](#create-a-branch-and-make-changes)
  - [Keeping your fork in sync](#keeping-your-fork-in-sync)
  - [Committing changes to your fork](#committing-changes-to-your-fork)
  - [Creating a pull request](#creating-a-pull-request)
  - [Getting a code review](#getting-a-code-review)
  - [When to retain commits and when to squash](#when-to-retain-commits-and-when-to-squash)
- [About testing](#about-testing)
- [Project conventions](./conventions.md)
- [Creating fast and lean code](./performance.md)

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
```

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

## Building the code

To build the mixer, enter:

```
cd $(ISTIO)/mixer
bazel build :all
```

This should figure out what it needs to do and not need any input from you.
You can delete any build artifacts with:

```
bazel clean
```
You can run all the available tests with:

```
bazel test ...
```

## About testing

Before sending pull requests you should at least make sure your changes have
passed both unit and integration tests. We only merges pull requests when
**all** tests are passing.

* Unit tests should be fully hermetic
  - Only access resources in the test binary.
* All packages and any significant files require unit tests.
* The preferred method of testing multiple scenarios or input is
  [table driven testing](https://github.com/golang/go/wiki/TableDrivenTests)
* Concurrent unit test runs must pass.

## Coding advice

  - [Go landmines](https://gist.github.com/lavalamp/4bd23295a9f32706a48f)
