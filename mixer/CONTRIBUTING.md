# Contributing guidelines

So, you want to hack on Istio Mixer? Yay!

## Developer Guide

We have a [Developer's Guide](docs/devel/development.md) that outlines everything
you need to know to contribute. If you find something undocumented or incorrect
along the way, please feel free to send a Pull Request.

## Filing issues

If you have a question about Istio Mixer or have a problem using it, please
[file an issue](https://github.com/istio/mixer/issues/new).

[//]: # (TODO: add troubleshooting guide)

## How to become a contributor and submit your own code

### Contributor License Agreements

We'd love to accept your patches! Before we can take them, we have to jump a
couple of legal hurdles.

Please fill out the Cloud Native Computing Foundation (CNCF) CLA.

CNCF:
  * To contribute as an individual or as am employee of a signed organization,
    [go here](https://identity.linuxfoundation.org/projects/cncf).
  * To sign up as an organization, [go
    here](https://identity.linuxfoundation.org/node/285/organization-signup).

Once you are CLA'ed, we'll be able to accept your pull requests.

***NOTE***: Only original source code from you and other people that have
signed the CLA can be accepted into the repository. This policy does not
apply to [third_party](third_party/) and [vendor](vendor/).

### Finding Things That Need Help

[//]: # (TODO: fill out)

### Contributing A Patch

If you're working on an existing issue, simply respond to the issue and express
interest in working on it. This helps other people know that the issue is
active, and hopefully prevents duplicated efforts.

If you want to work on a new idea of relatively small scope:

1. Submit an issue describing your proposed change to the repo in question.
1. The repo owners will respond to your issue promptly.
1. If your proposed change is accepted, and you haven't already done so, sign a
   Contributor License Agreement (see details above).
1. Fork the repo, develop, and test your changes.
1. Submit a pull request.

TODO: document how to deal with bigger issues

### Downloading the project

There are a few ways you can download this code. You must download it into a
GOPATH - see [golang.org](https://golang.org/doc/code.html) for more info on
how Go works with code. This project expects to be found at the Go package
`github.com/istio/mixer/`.

1. You can `git clone` the repo. If you do this, you MUST make sure it is in
   the GOPATH as `github.com/istio/mixer` or it may not build.  E.g.: `git clone
   https://github.com/kubernetes/kubernetes $GOPATH/src/github.com/istio/mixer`
1. You can use `go get` to fetch the repo. This will automatically put it into
   your GOPATH in the right place. E.g.: `go get -d github.com/istio/mixer`


### Building the project

There are a few things you need to build and test this project:

1. `make` - the human interface to the Istio Mixer build is `make`, so you must
   have this tool installed on your machine. We try not to use too many crazy
   features of `Makefile`s and other tools, so most commonly available versions
   should work.
1. `go` - Istio Mixer is written in Go (aka golang), so you need a relatively
   recent version of the [Go toolchain](https://golang.org/dl/) installed.
   While Linux is the primary platform for Istio Mixer, it should compile on a
   Mac, too. Windows is in progress.

To build Istio Mixer, simply type `make`.  This should figure out what it needs
to do and not need any input from you.

To run basic tests, simply type `make test`.  This will run all of the unit
tests in the project.

### Protocols for Collaborative Development

Please read [this doc](docs/devel/collab.md) for information on how we're
running development for the project.  Also take a look at the [development
guide](docs/devel/development.md) for information on how to set up your
environment, run tests, manage dependencies, etc.

### Adding dependencies

[//]: # (TODO: add bits on glide here)

### Community Expectations

Please see our [expectations](docs/devel/community-expectations.md) for members
of the Istio Mixer community.