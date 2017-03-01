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
  - [Merge hours](#merge-hours)

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

Active contributors are considered to be anyone who meets any of the following
criteria:
   * Sent more than two pull requests (PRs) in the previous one month, or more
   than 20 PRs in the previous year.
   * Filed more than three issues in the previous month, or more than 30 issues
   in the previous 12 months.
   * Commented on more than 5 pull requests in the previous month, or
   more than 50 pull requests in the previous 12 months.
   * Marked any PR as LGTM in the previous month.
   * Have *collaborator* permissions in the respective repository.

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
and are strongly encouraged to go above and beyond the code of conduct to promote a
collaborative and respectful community.

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

Merging of PRs is done by the project maintainers. The current
list of maintainers is kept in the [OWNERS](OWNERS) file at the root of
each repository.

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
1. It has been `LGTM`-ed by at least one of the maintainers listed in
   the OWNERS file for that repository.
1. It has all appropriate corresponding documentation and tests.

### Merge hours

Maintainers will do merges of appropriately reviewed-and-approved changes during
their local "business hours" (typically 7:00 am Monday to 5:00 pm (17:00h)
Friday). PRs that arrive over the weekend or on holidays will only be merged if
there is a very good reason for it and if the code review requirements have been
met. Concretely this means that nobody should merge changes immediately before
going to bed for the night.

There may be discussion and even approvals granted outside of the above hours,
but merges will generally be deferred.

If a PR is considered complex or controversial, the merge of that PR should be
delayed to give all interested parties in all timezones the opportunity to
provide feedback. Concretely, this means that such PRs should be held for 24
hours before merging. Of course "complex" and "controversial" are left to the
judgment of the people involved, but we trust that part of being a committer is
the judgment required to evaluate such things honestly, and not be motivated by
your desire (or your cube-mate's desire) to get their code merged. Any reviewer can
issue a ["hold"](#holds) to indicate that the PR is in
fact complicated or complex and deserves further review.

PRs that are incorrectly judged to be merge-able, may be reverted and subject to
re-review, if subsequent reviewers believe that they in fact are controversial
or complex.
