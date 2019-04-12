# PR Author's Guide to Tide

If you just want to figure out how to get your PR to merge this is the document for you!

## Sources of Information

1. The `tide` status context at the bottom of your PR.
The status either indicates that your PR is in the merge pool or explains why it is not in the merge pool. The 'Details' link will take you to either the Tide or PR dashboard.
![Tide Status Context](/prow/cmd/tide/status-context.png)
1. The PR dashboard at "`<deck-url>`/pr" where `<deck-url>` is something like "https://prow.k8s.io".
This dashboard shows a card for each of your PRs. Each card shows the current test results for the PR and the difference between the PR state and the merge criteria. [K8s PR dashboard](https://prow.k8s.io/pr)
1. The Tide dashboard at "`<deck-url>`/tide".
This dashboard shows the state of every merge pool so that you can see what Tide is currently doing and what position your PR has in the retest queue. [K8s Tide dashboard](https://prow.k8s.io/tide)

## Get your PR merged by asking these questions

#### "Is my PR in the merge pool?"

If the `tide` status at the bottom of your PR is successful (green) it is in the merge pool. If it is pending (yellow) it is *not* in the merge pool.

#### "Why is my PR not in the merge pool?"

First, if you just made a change to the PR, give Tide a minute or two to react. Tide syncs periodically (1m period default) so you shouldn't expect to see immediate reactions.

To determine why your PR is not in the merge pool you have a couple options.
1. The `tide` status context at the bottom of your PR will describe at least one of the merge criteria that is not being met. The status has limited space for text so only a few failing criteria can typically be listed. To see all merge criteria that are not being met check out the PR dashboard.
1. The PR dashboard shows the difference between your PR's state and the merge criteria so that you can easily see all criteria that are not being met and address them in any order or in parallel.


#### "My PR is in the merge pool, what now?"

Once your PR is in the merge pool it is queued for merge and will be automatically retested before merge if necessary. So **typically your work is done!**
The one exception is if your PR fails a retest. This will cause the PR to be removed from the merge pool until it is fixed and is passing all the required tests again.

If you are eager for your PR to merge you can view all the PRs in the pool on the Tide dashboard to see where your PR is in the queue. Because we give older PRs (lower numbers) priority, it is possible for a PR's position in the queue to increase.

Note: Batches of PRs are given priority over individual PRs so even if your PR is in the pool and has up-to-date tests it won't merge while a batch is running because merging would update the base branch making the batch jobs stale before they complete.
Similarly, whenever any other PR in the pool is merged, existing test results for your PR become stale and a retest becomes necessary before merge. However, your PR remains in the pool and will be automatically retested so this doesn't require any action from you.
