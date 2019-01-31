# Contributing

Thank you for your interest in contributing to the Go implementation of CEL!

# Where to Start?

CEL is a dynamic expression language that parses into a portable format,
and lends itself well to evaluation across processes through its support
of evaluation with partial state.

We currently welcome a wide variety of contributions:

* Requests for bugs and features through issues.
* Fixes of bugs and implementations of features.
* Documentation and examples.

The general guideline is that contributions must improve the user experience
without significantly altering its semantics or performance characteristics.

Learn more about CEL semantics in the [CEL Spec][1] repo.

# Contribution Process

All submissions require review. Small changes can be submitted directly via
[Pull Request](./PULL_REQUEST_TEMPLATE.md) on GitHub. Use your judgement about
what constitutes a small change, but if in doubt, follow this process for
larger changes:

* Determine whether the changes has already been requested in Issues.
* If so, please add a comment to indicate your interest as this helps
  the CEL maintainers prioritize changes.
* If not, file an [Issue](./ISSUE_TEMPLATE.md).

If you are still in doubt about whether to file an Issue or would like to
consult with the broader CEL Go community, feel free to send a message to
the [CEL Go Discuss][2] Google group.

## Code Contributions

See [PULL_REQUEST_TEMPLATE.md](./PULL_REQUEST_TEMPLATE.md) for guidelines on
creating, reviewing, and merging code contributions.

### Contributor License Agreement

Code contributions must be accompanied by a Contributor License Agreement. You
(or your employer) retain the copyright to your contribution, but the agreement
gives us permission to use and redistribute your contributions as part of the
project. Visit <https://cla.developers.google.com/> to view priot agreements,
or to sign a new one.

You generally only need to submit a CLA once, so if you've already submitted
one (even if it was for a different project), you probably don't need to do it
again.

## Code Reviews

All code changes must be reviewed before merge. Expect maintainers to respond
to new issues or pull requests within a week.

If approval is given to a code change, commits should be squashed on merge.
This makes it easier to triage issues related to a change, and makes the commit
graph human-readable.

For outstanding and ongoing issues and particularly for long-running pull
requests, expect the maintainers to review within a week of a contributor
asking for a new review. There is no commitment to resolution -- merging
or closing a pull request, or fixing or closing an issue -- because some
issues will require more discussion than others.

[1]:  https://github.com/google/cel-spec
[2]:  https://groups.google.com/forum/#!forum/cel-go-discuss