# Reviewing and Merging Pull Requests

This document is a guideline for how the project's maintainers will review 
issues and merge pull requests (PRs).

## Project Maintainers

Reviewing of issues and PRs is done by the projects maintainers. The current
list of maintainers is kept in the [OWNERS](OWNERS) file at the root of
each repository.

Like many opensource projects, becoming a maintainer is based on contributions
to the project. Active contributors to the project will eventually get noticed
by the maintainers, and one of the existing maintainers will nominate the
contributor to become a maintainer.  A 'yes' vote from >75% of
the existing maintainers is required for approval.

Removing a maintainer requires a 'yes' vote from >75% of the exsiting
maintainers. Note that inactive maintainers might periodically be removed
simply to keep the membership list accurate.

## Reviewing

Once a PR has been submitted reviewers should attempt to do an initial review
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
justification for why they are not in favor of the change.

If a PR gets a `NOT LGTM` vote then this issue should be discussed among
the group to try to resolve their differences.

## Merging PRs

PRs may only be merged after the following criteria are met:

1. It has been open for at least 1 business day
1. It has no `NO LGTM` comment from a reviewer
1. It has been `LGTM`-ed by at least one of the maintainers listed in
   the OWNERS file for that repository
1. It has all appropriate corresponding documentation and testcases

