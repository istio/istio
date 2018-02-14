# Istio On-call Playbook

## Who
Every week, one Istio developer is on-call and is responsible for maintaining Istio build and help out users and other developers. The on-call person should prioritize on-call duties on top of their daily work.

## Schedule
The on-call schedule can be found [here][https://docs.google.com/spreadsheets/d/1FaHwPpad3F3hva2suJweNeTnocjKtLnbgLkyMRPzgUY/edit#gid=1475801904].

On-call duty starts on Tuesday at noon PST, ends the following week on Tuesday at noon PST, and is performed during regular working hours for your time zone.

## Prerequisites
* Follow the [Istio dev guide][https://github.com/istio/istio/blob/master/DEV-GUIDE.md] to set up your environment
* Join google group [istio-users][https://groups.google.com/forum/#!forum/istio-users]
* Join google groups [istio-oncall][https://groups.google.com/forum/#!forum/istio-oncall]
* Join the `oncall` Slack channel

## Responsibilities
* Build cop: monitor the builds and the automated tests, helps PRs be merged
  * Familiarize yourself with the current [open issues affecting automated tests][https://github.com/istio/istio/issues?q=is%3Aopen+is%3Aissue+label%3Akind%2Ftest-failure].
  * If there are new failures, open issues and label them with kind/test-failure, the appropriate area label, "prow" or "circleci" label, and assign them either directly to an engineer or to the lead.
  The issue must contain the link to the failed test log.
  Add a comment in github cc-ing the assignees and explaining this must be fixed or reverted with highest priority. Nag people when needed.
  * May need to merge PRs that fail tests with known failures, but must document in the PR and link to the known issue: "Failure in e2e-... test is due to unrelated issue #"
* Slack: monitor #oncall channel; Update the topic of the channel, so that everyone can see who is on-call.
* Istio-users mailing list: monitor, and reply or dispatch to the subject matter experts or to the leads.
* Use this on-call time to learn something new about Istio. Work with the subject matter experts and contribute to the troubleshooting guides on [istio.io][https://istio.io/help/troubleshooting.html].
* Help with creating the release when needed.
* Check the schedule sheet to make sure the next on call is defined.
* On Friday, notify the next on-call.
* On Tuesday morning, handoff to next oncall and send a summary to istio-dev containing these stats, that can be obtained by querying [github with dates ranges][https://help.github.com/articles/searching-issues-and-pull-requests/]

```bash
#Issues affecting automated tests:
0 baseline / 21 incoming / 4 fixed / 17 open

#Issues total
488 baseline / 526 current

#PRs with review approved / in flight:
~53 baseline / 22 current
```


