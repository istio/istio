package pullrequest

import (
	"context"
	"log"
	"time"

	"github.com/google/go-github/github"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/controller-tools/examples/godocbot/pkg/apis/code/v1alpha1"
)

// GithubSyncer implements following functionalities:
//  - Watches newly created PRs in K8s and updates their commitID by calling
//  Github
//  - Periodically updates the PRs in K8s with their commitID in Github.
type GithubSyncer struct {
	mgr  manager.Manager
	ctrl controller.Controller
	// TODO(droot): take make GithubClient interface to make it easy to test the
	// syncer
	ghClient *github.Client
	// TODO(droot): parameterize the sync duration
	syncInterval time.Duration
}

func NewGithubSyncer(
	mgr manager.Manager,
	enablePRSync bool,
	stop <-chan struct{}) (*GithubSyncer, error) {

	ghClient := github.NewClient(nil)

	ctrl, err := controller.New(
		"github-pullrequest-syncer",
		mgr,
		controller.Options{
			Reconcile: &pullRequestCommitIDReconciler{
				Client:   mgr.GetClient(),
				ghClient: ghClient,
			},
		})
	if err != nil {
		return nil, err
	}
	syncer := &GithubSyncer{
		mgr:          mgr,
		ctrl:         ctrl,
		ghClient:     ghClient,
		syncInterval: 30 * time.Second,
	}

	// Watch PullRequests objects
	if err := syncer.ctrl.Watch(
		&source.Kind{Type: &v1alpha1.PullRequest{}},
		&handler.Enqueue{}); err != nil {
		return nil, err
	}

	if enablePRSync {
		go syncer.Start(stop)
	}
	return syncer, nil
}

// pullRequestCommitIDReconciler reconciles commitID of newly created
// pullrequests in K8s.
type pullRequestCommitIDReconciler struct {
	Client client.Client
	// TODO(droot): take GithubClient interface to improve testability
	ghClient *github.Client
}

func (r *pullRequestCommitIDReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	ctx := context.Background()

	// Fetch PullRequest object
	pr := &v1alpha1.PullRequest{}
	err := r.Client.Get(ctx, request.NamespacedName, pr)
	if errors.IsNotFound(err) {
		log.Printf("Could not find PullRequest %v.\n", request)
		return reconcile.Result{}, nil
	}

	if err != nil {
		log.Printf("Could not fetch PullRequest %v for %+v\n", err, request)
		return reconcile.Result{}, err
	}

	if pr.Spec.CommitID != "" {
		// We already know the commit-id for the PR, so no need to update the commitID
		return reconcile.Result{}, nil
	}

	// Looks like this is a fresh PR, so lets determine the latest commitID
	prinfo, err := parsePullRequestURL(pr.Spec.URL)
	if err != nil {
		log.Printf("error in parsing the request, ignoring this pr %v err %v", request.NamespacedName, err)
		return reconcile.Result{}, nil
	}

	log.Printf("fetching commit id for the PR: %v", prinfo)
	ghPR, _, err := r.ghClient.PullRequests.Get(context.Background(), prinfo.org, prinfo.repo, int(prinfo.pr))
	if err != nil {
		log.Printf("error fetching PR details from github: %v", err)
		return reconcile.Result{}, err
	}

	pr.Spec.CommitID = ghPR.Head.GetSHA()
	err = r.Client.Update(context.Background(), pr)
	if err != nil {
		log.Printf("error updating PR github: %v", err)
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// Start periodically syncs PRs in k8s with their commitID in Github.
func (gs *GithubSyncer) Start(stop <-chan struct{}) {
	ticker := time.NewTicker(gs.syncInterval)
	for {
		select {
		case <-stop:
			/* we got signalled */
			return
		case <-ticker.C:
			gs.updateAllPullRequestCommits()
		}
	}
}

// updateAllPullRequestCommits performs the following:
//  - fetches all the PRs registered in K8s
//  - Organize them by orgs/repo so that batch calls can be made
//  - Fetches details of PRs from Github and updates the commitID of the PRs in
//  k8s if required.
// TODO(droot): delete the PRs from K8s if closed in Github.
func (gs *GithubSyncer) updateAllPullRequestCommits() {

	// get pull requests in all namespaces
	prList := &v1alpha1.PullRequestList{}
	err := gs.mgr.GetClient().List(context.Background(), client.InNamespace(metav1.NamespaceAll), prList)
	if err != nil {
		log.Printf("error fetching all the PRs from k8s: %v", err)
		return
	}

	orgs := pullRequestByOrgAndRepo(prList)
	for org, repos := range orgs {
		for repo, prs := range repos {

			ghPRs, err := githubPRsForRepo(gs.ghClient, org, repo)
			if err != nil {
				log.Printf("cannot get PRs for org '%s' and repo '%s' from GH: %v", org, repo, err)
				continue
			}

			for prNum, pr := range prs {
				if ghPR, found := ghPRs[prNum]; found &&
					ghPR.Head.GetSHA() != pr.Spec.CommitID {
					// PR has been updated in GitHub
					pr.Spec.CommitID = ghPR.Head.GetSHA()
					err = gs.mgr.GetClient().Update(context.Background(), pr)
					if err != nil {
						log.Printf("error updating PR github: %v", err)
						continue
					}
				}
			}
			// TODO(droot): if a PR is not found in GH, it is closed, so we need to probably
			// delete that PR from k8s cluster
		}
	}
}

// githubPRsForRepo fetches PRs for a given org and repo and indexes them by
// their pull request number.
func githubPRsForRepo(ghc *github.Client, org, repo string) (map[int64]*github.PullRequest, error) {
	m := map[int64]*github.PullRequest{}

	prs, _, err := ghc.PullRequests.List(context.Background(), org, repo, nil)
	if err != nil {
		return nil, err
	}

	for _, pr := range prs {
		m[int64(pr.GetNumber())] = pr
	}
	return m, nil
}

// this function organizes all the PRs present in K8s by repo names and orgs, so that
// we can make batch calls to Github. It returns map organized as follows:
//   {
//		"org-1": {
//			"repo-1": {
//				pull-request-number-1: "commit-id-1",
//				pull-request-number-2: "commit-id-2",
//				...other pull request to follow...
//			},
//			"repo-2": {
//				pull-request-number-1: "commit-id-1",
//				pull-request-number-2: "commit-id-2",
//				...other pull request to follow...
//			}
//			....other repos to follow....
//		}
//	    ....other orgs to follow....
//	}
// TODO(droot): explore if indexing available under pkg/cache can help us achive
// this organization.
func pullRequestByOrgAndRepo(prs *v1alpha1.PullRequestList) map[string]map[string]map[int64]*v1alpha1.PullRequest {
	orgs := map[string]map[string]map[int64]*v1alpha1.PullRequest{}
	for _, pr := range prs.Items {
		prinfo, _ := parsePullRequestURL(pr.Spec.URL)
		_, orgFound := orgs[prinfo.org]
		if !orgFound {
			orgs[prinfo.org] = map[string]map[int64]*v1alpha1.PullRequest{}
		}
		_, repoFound := orgs[prinfo.org][prinfo.repo]
		if !repoFound {
			orgs[prinfo.org][prinfo.repo] = map[int64]*v1alpha1.PullRequest{}
		}
		orgs[prinfo.org][prinfo.repo][prinfo.pr] = &pr
	}
	return orgs
}
