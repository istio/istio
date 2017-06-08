# Contribution guidelines

So, you want to hack on Istio? Yay!

The following sections outline the process by which all changes to the Istio
repositories will go through.  All changes, regardless of whether they are from
newcomers to the community or from the core team follow the exact
same process and are given the same level of review.

- [Code of conduct](#code-of-conduct)
- [Contributor license agreements](#contributor-license-agreements)
- [Issues](#issues)
- [Contributing a feature](#contributing-a-feature)
- [Pull requests](#pull-requests)

## Code of conduct

All members of the Istio community must abide by the
[CNCF Code of Conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).
Only by respecting each other can we develop a productive, collaborative community.

## Contributor license agreements

We'd love to accept your patches! But before we can take them, you will have
to fill out the [Google CLA](https://cla.developers.google.com).

Once you are CLA'ed, we'll be able to accept your pull requests. This is
necessary because you own the copyright to your changes, even after your
contribution becomes part of this project. So this agreement simply gives us
permission to use and redistribute your contributions as part of the project.

## Issues

GitHub issues can be used to report bugs or feature requests.

When reporting a bug please include the following key pieces of information:
- the version of the project you were using (e.g. version number,
  git commit, ...)
- operating system you are using
- the exact, minimal, steps needed to reproduce the issue.
  Submitting a 5 line script will get a much faster response from the team
  than one that's hundreds of lines long.

See the next section for information about using issues to submit
feature requests.

## Contributing a feature

If you would like to propose a new feature for the project then it would be
best to first discuss your idea with the community to gauge their level of
interest. You can use any of the communication channels to have this
discussion, but ideally a new GitHub issue should be opened so that the
history of the discussions can be saved within the repository itself.
The issue should include information about the requirements and
use cases that it is trying to address.

If you would like to also work on the implementation of the feature then
it should include a discussion of the proposed design and technical details
of how it will be implemented.

Before checking in a new feature, you are expected to contribute full
documentation for this feature within the main Istio repo (https://github.com/istio/istio).

Once the idea has be discussed and there is a general agreement on the
technical direction, a PR can then be submitted.

If you would like to skip the process of submitting an issue and
instead would prefer to just submit a pull request with your desired
code changes then that's fine. But keep in mind that there is no guarantee
of it being accepted and so it is usually best to get agreement on the
idea/design before time is spent coding it. However, sometimes seeing the
exact code change can help focus discussions, so the choice is up to you.

## Pull requests

If you're working on an existing issue, simply respond to the issue and express
interest in working on it. This helps other people know that the issue is
active, and hopefully prevents duplicated efforts.

To submit a proposed change:
- Setup your [development environment](devel/README.md).
- Fork the repository.
- Create a new branch for your changes.
- Develop the code/fix.
- Add new test cases. In the case of a bug fix, the tests should fail
  without your code changes. For new features try to cover as many
  variants as reasonably possible.
- Modify the documentation as necessary.
- Verify the entire CI process (building and testing) work.

While there may be exceptions, the general rule is that all PRs should
be 100% complete - meaning they should include all test cases and documentation
changes related to the change.

When ready, if you have not already done so, sign a
[contributor license agreements](#contributor-license-agreements) and submit 
the PR.

See [Reviewing and Merging Pull Requests](REVIEWING.md) for the PR review and
merge process that is used by the maintainers of the project.
