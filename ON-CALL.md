# Istio On-call Playbook

## Who
Every week, one Istio developer is on-call and is responsible for maintaining the Istio build process and
for helping out users and other developers. The on-call person should prioritize on-call duties on top of their daily work.

## Schedule
The on-call schedule can be found [here](https://docs.google.com/spreadsheets/d/1FaHwPpad3F3hva2suJweNeTnocjKtLnbgLkyMRPzgUY/edit#gid=1475801904).

On-call duty starts on Tuesday at noon PST your time zone, ends the following week on Tuesday at noon PST your time zone,
and is performed during regular working hours for your time zone.

## Prerequisites
* Follow the [Istio dev guide](https://github.com/istio/istio/blob/master/DEV-GUIDE.md) to set up your environment
* Join google group [istio-users](https://groups.google.com/forum/#!forum/istio-users)
* Join google groups [istio-on-call](https://groups.google.com/forum/#!forum/istio-oncall)
* Join the `oncall` Slack channel

## Responsibilities
* Build cop: monitor the builds, the presubmit automated tests, the postsubmit automated tests:
  * Familiarize yourself with the current [open issues affecting automated tests](https://github.com/istio/istio/issues?q=is%3Aopen+is%3Aissue+label%3Akind%2Ftest-failure).
  * Monitor the [dashboard](http://k8s-testgrid.appspot.com/istio#Summary)
  * If there are new failures, open issues and label them with kind/test-failure, with the appropriate area label, with "prow" or "circleci" label,
  and assign them either directly to an engineer or to the area lead.
  The issue must contain a link to the failed test log.
  Add a comment in GitHub cc-ing the assignees and explaining this must be fixed or reverted with highest priority. Nag people when needed.
  * Help PRs be merged: merge PRs that fail tests with known failures, but document the failure with link to the known issue: "Failure in e2e-... test is due to unrelated issue #"
* Slack: monitor #oncall channel; Update the topic of the channel, so that everyone can see who is on-call.
* Istio-users mailing list: monitor, and reply or dispatch to the subject matter experts or to the leads.
* Use this on-call time to learn something new about Istio. Work with the subject matter experts and contribute to the troubleshooting guides on [istio.io](https://istio.io/help/troubleshooting.html).
* Help with creating the release when needed.
* Check the schedule sheet to make sure the next on call is defined.
* On Friday, notify the next on-call.
* On Tuesday, at the end of the on-call, handoff to next on-call and send a summary to istio-oncall and istio-dev. Include the stats below, that can be obtained by querying [GitHub with dates ranges:](https://help.github.com/articles/searching-issues-and-pull-requests/)

* At the begining of your on-call make sure https://goo.gl/9wjRdg is updated with current snapshot numbers (of issues, PRs, etc...)
* At the end of your on-call, update https://goo.gl/9wjRdg - the deltas will be calculated for you (bold columns) please send an email summary

```bash
#Issues affecting automated tests:
0 baseline / 21 incoming / 4 fixed / 17 open

#Issues total
488 baseline / 526 current

#PRs with review approved / in flight:
53 baseline / 22 current
```

## Instructions for temporarily disabling required tests in GitHub

Note: You need admin permissions in Github to do this.

If Prow/CircleCI is impacted by test failures affecting multiple PRs, or there are some test/infrastructure issues,
you can disable that tests temporarily, but only after filing a kind/test-failure issue.
This would make these tests as "not mandatory" to pass before a PR is merged.

In istio/istio repo:
* Go to Settings
* Click branches (see settings-branches-master.png)
* Click on the Edit button next to master
* Scroll down to the section "Require status checks to pass before merging"
* Uncheck the affected tests.
