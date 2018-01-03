# Istio Release

- [Istio Release](#istio-release)
  * [Overview](#overview)
  * [Request Emergency Bug Fix and Patch Release](#request-emergency-bug-fix-and-patch-release) 

## Overview

Starting with [0.3.0](https://github.com/istio/istio/releases/tag/0.3.0), Istio is released and published every month and the
monthly releases can all be found on https://github.com/istio/istio/releases. Each monthly release has its own minor version
number, while the patch version indicates important bug fixes in the corresponding monthly release. You can find more
information about version semantic, cadence, and support in [Istio Release Principles](https://goo.gl/dcSBxF).

Internally, Istio releases are cut, tested, and qualified every day. And once a week, a daily release will go through
stability and performance testing to detect regression. The monthly release mentioned above is promoted from a weekly release
that passed all functional, stability, and performance testing. You can find more information about this process in
[Istio OSS release train](https://goo.gl/6V1SHm).

The test/release working group is currently working on release train automation. As of 2017 Q4, it is in alpha state that
produces daily, monthly, and patch releases automatically, under manual supervision. Once the process is all ironed out, 
daily, weekly, and monthly releases will be entirely automatic. Patch releases can be requested by developers to fix severe
bugs.

## Request Emergency Bug Fix and Patch Release

When a severe bug is found, developers can contact hklai@google.com and vickyxu@google.com to create a patch release on all
[currently supported releases](https://docs.google.com/document/d/1scC_f7nObwBut_xxE6lUfvcHBybmttUsJDMwXD3gcO4/edit#heading=h.b56ea7aggz9t). Only bugs that
affect stability and security are qualified, while minor problems and new features must wait for the next monthly release.

Currently, the test/release group handles all patch releases. In future, we expect to distribute that work load to developer
on-call.
