# Istio Release Process

- [Istio Release Process](#istio-release-process)
  * [Overview](#overview)
  * [Daily Releases](#daily-releases)
  * [Weekly Releases](#weekly-releases)
  * [Monthly Releases](#monthly-releases)
  * [Quarterly Releases](#quarterly-releases)
  * [Patch Releases](#patch-releases)

## Overview

Starting with [0.3.0](https://github.com/istio/istio/releases/tag/0.3.0), Istio is released and published every month. The
monthly releases can all be found on https://github.com/istio/istio/releases. You can find more
information about version semantic, cadence, and support in [Istio Release Cadence](https://istio.io/about/release-cadence/).

Internally, Istio releases are cut, tested, and qualified every day. And once a week, a daily release will go through
stability and performance testing to detect regression. The monthly release mentioned above is promoted from a weekly release
that passed all functional, stability, and performance testing. You can find more information about this process in
[Istio OSS release train](https://goo.gl/6V1SHm).

The test/release working group is currently working on release train automation. As of 2018 Q2, it is in alpha state that
produces daily, monthly, and patch releases automatically, under manual supervision. Once the process is all ironed out,
daily, weekly, and monthly releases are expected to be automatic. Patch releases can be requested by developers to fix severe
bugs.

## Daily Releases

Every day, a new release is automatically built and made available at 
https://gcsweb.istio.io/gcs/istio-prerelease/daily-build/
after it passes the automatic daily release tests. These are meant for developer testing, but not general consumption.

Each daily release is versioned and identified by ```<branch_name>-<build_datetime>```. For example, E.g. 
```master-20180615-09-15``` is cut from master at 9:15AM GMT on June 15, 2018. Daily release artifacts are stored in 
sub-directories using the same naming scheme in [GCS](https://gcsweb.istio.io/gcs/istio-prerelease/daily-build/).


## Weekly Releases

The daily releases built on 7th, 14th, 21st, and 28th are used as weekly releases. If there is no daily release on that day,
as a result of issues like broken build and test failures, the last good daily release will be used as the weekly release
instead. Istio developers are expected to restore the build or failing tests ASAP.

We are in the process of automating weekly releases to go through a week long rigorous stability and performance testing. 
(ETC Q2)

## Monthly Releases

Every month, the second weekly release (i.e. the daily release built on 14th of the month) is used as the monthly release
candidate, which will then be subject to additional manual testing by the community for about a week.

The [Istio release group leads](https://github.com/istio/community/blob/master/WORKING-GROUPS.md#test-and-release) will 
announce the monthly release candidate after it is selected, and create a spreadsheet for community members to sign up for
their test areas, and share results. If there are no major blockers, the candidate will be rebuilt and relabeled as the
official monthly release, on around 22nd (or the following working day) of the month.

If [critical bugs](#critical-bug-definition) are found in the release candidate, github issues must be filed using the
**“kind/blocking release”** label. Similarly, any other release blocking issues must also be filed and labeled accordingly.
These issues must contain details about why it’s considered critical, when it was first encountered, repro steps, root cause
(if known), workaround (if any), ETA, and assignees who are actively working on it. New features are not release blocking.

The [Istio release group leads](https://github.com/istio/community/blob/master/WORKING-GROUPS.md#test-and-release) will review 
these issues proactively and decide if they are indeed release blocking. The bugs that are not considered as critical will 
only be addressed by adding to the known issues section in the corresponding release notes.

[Critical bugs](#critical-bug-definition) fixes will be cherry picked in the release candidate branch, to reduce the risk of
including additional bugs. In case the release candidate cannot be stabilized within a week, the 
[Istio release group leads](https://github.com/istio/community/blob/master/WORKING-GROUPS.md#test-and-release) will declare
[Code Yellow](#code-yellow).

### Code Yellow

The first release candidate is abandoned, and the next weekly release (i.e. the daily release built on 21th of the month) is
used as the monthly release candidate instead. Stabilizing the release candidate is the **first priority** of all Istio
developers, and the goal is to stabilize the release candidate **ASAP**.

In case the second release candidate cannot be stabilized within a week, the 
[Istio release group leads](https://github.com/istio/community/blob/master/WORKING-GROUPS.md#test-and-release) will declare
[Code Orange](#code-orange).

**Exit criteria:** Code yellow is over when a new monthly release is shipped.

### Code Orange
The second release candidate is again abandoned, and the next weekly release (i.e. the daily release built on 28th of the
month) is used as the monthly release candidate instead. Stabilizing the release candidate is the **only priority** of all
Istio developers, and the goal is to stabilize the release candidate **at all cost**. The monthly release will most certainly
be slipped to the next calendar month, and reduce the available development time for the next monthly release.

At the discretion of TOC, a code freeze may be declared, and a SWAT team may be formed to help expedite the stabilization.

**Exit criteria:** Code orange is over when a new monthly release is shipped.

## Quarterly Releases
Every quarter, a monthly release is deemed as the LTS (Long Term Support) release. These releases are typically more stable
than regular monthly releases and are safe to deploy to production. Users are encouraged to upgrade to these releases ASAP.

These releases are clearly marked as LTS on [github releases page](https://github.com/istio/istio/releases) as well as 
[release notes](https://istio.io/about/notes/).

## Patch Releases
When a [critical bugs](#critical-bug-definition) is found in a monthly release, developers can contact the
[Istio release group leads](https://github.com/istio/community/blob/master/WORKING-GROUPS.md#test-and-release)
to create a patch release. Minor problems and new features must wait for the next monthly release.

When a patch release is planned, the
[Istio release group leads](https://github.com/istio/community/blob/master/WORKING-GROUPS.md#test-and-release) will create a
github issue for tracking purposes, and communicate the plan to istio-dev@.

Currently, the test/release group handles all patch releases. In future, we expect that building patch releases will be done
by the developer on-call rotation.

### Critical Bug Definition
- Regression of a feature that is commonly used by customers in prior releases.
- Runtime crashes that severely regress stability of the system (i.e. SLO)

