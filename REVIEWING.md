# Reviewing and Merging Pull Requests

As a community we believe in the value of code reviews for all contributions.
Code reviews increase both the quality and readability of our code base, which
in turn produces high quality software.

This document provides guidelines for how the project's maintainers review
issues and merge pull requests (PRs).

- [Pull requests welcome](#pull-requests-welcome)
- [Code reviewers](#code-reviewers)
- [Reviewing changes](#reviewing-changes)
  - [Holds](#holds)
- [Project maintainers](#project-maintainers)
- [Merging PRs](#merging-prs)

## Pull requests welcome

First and foremost: as a potential contributor, your changes and ideas are
welcome at any hour of the day or night, weekdays, weekends, and holidays.
Please do not ever hesitate to ask a question or send a PR.

### Code reviewers

The code review process can introduce latency for contributors
and additional work for reviewers that can frustrate both parties.
Consequently, as a community we expect that all active participants in the
community will also be active reviewers. We ask that active contributors to
the project participate in the code review process in areas where that
contributor has expertise.

## Reviewing changes

Once a PR has been submitted, reviewers should attempt to do an initial review
to do a quick "triage" (e.g. close duplicates, identify user errors, etc.),
and potentially identify which maintainers should be the focal points for the
review.

If a PR is closed, without accepting the changes, reviewers are expected
to provide sufficient feedback to the originator to explain why it is being
closed.

During a review, PR authors are expected to respond to comments and questions
made within the PR - updating the proposed change as appropriate.

After a review of the proposed changes, reviewers may either approve
or reject the PR. To approve they should add a `LGTM` comment to the
PR. To reject they should add a `NOT LGTM` comment along with a full
justification for why they are not in favor of the change. If a PR gets
a `NOT LGTM` vote then this issue should be discussed among
the group to try to resolve their differences.

Because reviewers are often the first points of contact between new members of
the community and can therefore significantly impact the first impression of the
Istio community, reviewers are especially important in shaping the
community. Reviewers are highly encouraged to review the
[code of conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md)
and are strongly encouraged to go above and beyond the code of conduct to
promote a collaborative and respectful community.

Reviewers are expected to respond in a timely fashion to PRs that are assigned
to them. Reviewers are expected to respond to *active* PRs with reasonable
latency, and if reviewers fail to respond, those PRs may be assigned to other
reviewers. *Active* PRs are considered those which have a proper CLA (`cla:yes`)
label and do not need rebase to be merged. PRs that do not have a proper CLA, or
require a rebase are not considered active PRs.

### Holds

Any maintainer or core contributor who wants to review a PR but does not have
time immediately may put a hold on a PR simply by saying so on the PR discussion
and offering an ETA measured in single-digit days at most. Any PR that has a
hold shall not be merged until the person who requested the hold acks the
review, withdraws their hold, or is overruled by a preponderance of maintainers.

## Project maintainers

Merging of PRs is done by the project maintainers.

Like many open source projects, becoming a maintainer is based on contributions
to the project. Active contributors to the project will eventually get noticed
by the maintainers, and one of the existing maintainers will nominate the
contributor to become a maintainer.  A 'yes' vote from >75% of
the existing maintainers is required for approval.

Removing a maintainer requires a 'yes' vote from >75% of the exsiting
maintainers. Note that inactive maintainers might periodically be removed
simply to keep the membership list accurate.

## Merging PRs

PRs may only be merged after the following criteria are met:

1. It has been open for at least 1 business day.
1. It has no `NO LGTM` comment from a reviewer.
1. It has been `LGTM`-ed by at least one of the maintainers of that repository.
1. It has all appropriate corresponding documentation and tests.
