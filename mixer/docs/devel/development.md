# Development Guide

This document is intended to be the canonical source of truth for things like
supported toolchain versions for building Istio Mixer. If you find a
requirement that this doc does not capture, please
[submit an issue](https://github.com/istio/mixer/issues) on github. If
you find other docs with references to requirements that are not simply links to
this doc, please [submit an issue](https://github.com/istio/mixer/issues).

This document is intended to be relative to the branch in which it is found.
It is guaranteed that requirements will change over time for the development
branch, but release branches of Istio Mixer should not change.

## Building Istio Mixer

Please see our [build docs](../building.md).
	
### Go development environment

Istio Mixer is written in the [Go](http://golang.org) programming language.
To build Istio Mixer, you'll need a Go development environment. Builds for 
Istio Mixer require Go version 1.7.0. If you haven't set up a Go development
environment, please follow [these instructions](http://golang.org/doc/code.html)
to install the go tools.

Set up your GOPATH and add a path entry for go binaries to your PATH. Typically
added to your ~/.profile:

```sh
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin
```

### Godep dependency management

Istio Mixer build and test scripts use [glide](https://github.com/Masterminds/glide) to
manage dependencies.

#### Install glide

To install glide, please follow their [installation guide](https://github.com/Masterminds/glide#install).

### Local build using make

To build Istio Mixer using your local Go development environment (generate linux
binaries):

```sh
make
```

You may pass build options and packages to the script as necessary. For example,
to build with optimizations disabled for enabling use of source debug tools:

```sh
make GOGCFLAGS="-N -l"
```

## Workflow

Below, we outline one of the more common git workflows that core developers use.
Other git workflows are also valid.

[//]: # (TODO: add visual overview)

### Fork the main repository

1. Go to https://github.com/istio/mixer
2. Click the "Fork" button (at the top right)

### Clone your fork

The commands below require that you have $GOPATH set ([$GOPATH
docs](https://golang.org/doc/code.html#GOPATH)). We highly recommend you put
Istio Mixer' code into your GOPATH. Note: the commands below will not work if
there is more than one directory in your `$GOPATH`.

```sh
mkdir -p $GOPATH/src/istio
cd $GOPATH/src/istio
# Replace "$YOUR_GITHUB_USERNAME" below with your github username
git clone https://github.com/$YOUR_GITHUB_USERNAME/mixer.git
cd mixer
git remote add upstream 'https://github.com/istio/mixer.git'
```

### Create a branch and make changes

```sh
git checkout -b my-feature
# Make your code changes
```

### Keeping your development fork in sync

```sh
git fetch upstream
git rebase upstream/master
```

Note: If you have write access to the main repository at
github.com/istio/mixer, you should modify your git configuration so
that you can't accidentally push to upstream:

```sh
git remote set-url --push upstream no_push
```

### Committing changes to your fork

TODO: iron out and detail pre-commit hooks.

Then you can commit your changes and push them to your fork:

```sh
git commit
git push -f origin my-feature
```

### Creating a pull request

1. Visit https://github.com/$YOUR_GITHUB_USERNAME/mixer
2. Click the "Compare & pull request" button next to your "my-feature" branch.
3. Check out the pull request [process](pull-requests.md) for more details

**Note:** If you have write access, please refrain from using the GitHub UI for
creating PRs, because GitHub will create the PR branch inside the main
repository rather than inside your fork.

### Getting a code review

Once your pull request has been opened it will be assigned to one or more
reviewers. Those reviewers will do a thorough code review, looking for
correctness, bugs, opportunities for improvement, documentation and comments,
and style.

Very small PRs are easy to review.  Very large PRs are very difficult to
review. Github has a built-in code review tool, which is what most people use.

[//]: # (TODO: add bits about fast path reviews)

### When to retain commits and when to squash

Upon merge, all git commits should represent meaningful milestones or units of
work. Use commits to add clarity to the development and review process.

Before merging a PR, squash any "fix review feedback", "typo", and "rebased"
sorts of commits. It is not imperative that every commit in a PR compile and
pass tests independently, but it is worth striving for. For mass automated
fixups (e.g. automated doc formatting), use one or more commits for the
changes to tooling and a final commit to apply the fixup en masse. This makes
reviews much easier.

## Testing

```sh
cd mixer
make test # Run every unit test
```

See the [testing guide](testing.md) for additional information and scenarios.

[//]: # (TODO: Add integration/end-to-end testing info)