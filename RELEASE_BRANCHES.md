# Release Branches

Release branches have a name of `release-MAJOR.MINOR`. Essential Istio repositories are branched from master roughly 4
weeks prior to a new release. The `istio/istio.io` repository does not get branched until the release is ready
for publication.

This document outlines getting in new features after a new branch has been cut and the process for getting a PR
merged in before and after the first public release.

# Feature Freeze

One week before a release, the release branch goes into a state of code freeze. At this point only critical release
blocking bugs are addressed. Additional changes that are targeted for new features and capabilities will not be merged.

## Features requiring API changes

If a PR change requires an API change
* Updates to documentation do not require explicit approval from the Technical Oversight Committee (TOC). This can be
  held off after the release. Hiding/Showing documentation does not require TOC approval.
* Cases for other API changes require TOC approval
    * Simple `meshConfig` changes have been approved in the past. Functionality should not be enabled by default.
* For large API changes, 2 members of the TOC must approve the PR before release manager approval in the release branch
  of the istio/api repository. This does not have to wait for the weekly TOC meeting.
    * Risk should be assessed in the PR.
        * Have installs and upgrades affected by this feature?
        * Is the feature still being worked on?
        * Is the default behavior altered?
        * Is this turned on by default?
        * How many users are affected by this change?

## Feature implementation

Release managers will continue to have a final say in what gets merged or not, unless directed by the TOC. See the next
section if the implementation is done, but a bug is being addressed.

* Any code that changes feature functionality must be done within 2 weeks of the release branch cutting. Even then it is
 at the discretion of the release managers if PRs will be accepted and merged into the release branch.
* All code merged in after the feature freeze must have an associated GitHub issue and a release note (exception for
  unit and integration test changes).
    * The release managers may not know of all the features being worked on for an Istio release. Developers must
      make their case as to why their PR should be merged.
* Developers submitting PRs after the code freeze and after the first release candidate must provide justification
for including it in the “.0” release. Otherwise, the PRs will not be merged until after the release.
* For large fixes (>100 LOC that’s not from generated files), SMEs from that area must also approve the PR.

## Bug Fixes

* Bug fixes will not be merged in until the first release has been published, unless it addresses a critical issue.
* All changes should have an associated GitHub issue and/or a release note.
* Large fixes, where the LOC>100 not withstanding unit tests changes, require subject matter expert approval.

# Backporting fixes

* Cherry-picking is discussed in the original PR and subject-matter experts have approved the backport.
    * Behavioral changes should be highly scrutinized, while typo fixes don't require that level of scrutiny.
* It is preferable that cherry-picks are done by the istio-testing bot.
    * Automated cherry-picks do not need subject-matter experts to approve if discussed in the original PR.
    * To trigger the bot cherry pick, either:
        * Apply the correct `cherrypick/release-X.XX` label to the PR, and the bot should pick it up.
        * Use an explicit PR comment command: `/cherry-pick release-X.XX`
        * It is strongly preferred to always apply the correct `cherrypick/` label manually to aid search and tracking, even if you use the comment command method.
* All changes should have an associated GitHub issue and/or a release note.
    * In the event that a bug cannot be automatically backported, the istio-testing bot creates an issue for a failed
    attempt and assigns it to the developer. This issue **is not** sufficient for requesting approval.
* For manual cherry-picks:
    * Large fixes, where the LOC>100 not withstanding unit tests changes, require subject-matter expert approval.
    * Small fixes should be handled by the release managers.
