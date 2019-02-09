// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/google/go-github/github"
)

const (
	doNotMergeLabel = "PostSubmit Failed/Contact Oncall"
)

var (
	ci = NewCIState()
	// SHARegex matches commit SHA's
	SHARegex = regexp.MustCompile("^[a-z0-9]{40}$")
	// ReleaseTagRegex matches release tags
	ReleaseTagRegex = regexp.MustCompile("^[0-9]+.[0-9]+.[0-9]+$")
)

// CIState defines constants representing possible states of
// continuous integration tests
type CIState struct {
	Success string
	Failure string
	Pending string
	Error   string
}

// NewCIState creates a new CIState
func NewCIState() *CIState {
	return &CIState{
		Success: "success",
		Failure: "failure",
		Pending: "pending",
		Error:   "error",
	}
}

// GetCIState does NOT trust the given combined output but instead walk
// through the CI results, count states, and determine the final state
// as either pending, failure, or success
func GetCIState(combinedStatus *github.CombinedStatus, skipContext func(string) bool) string {
	return GetReqquiredCIState(combinedStatus, nil, skipContext)
}

// GetReqquiredCIState does NOT trust the given combined output but instead walk
// through the CI results, count states, and determine the final state
// as either pending, failure, or success
func GetReqquiredCIState(combinedStatus *github.CombinedStatus,
	requiredChecks *github.RequiredStatusChecks,
	skipContext func(string) bool) string {
	var failures, pending, successes int
	for _, status := range combinedStatus.Statuses {
		if requiredChecks != nil &&
			!IsRequiredCICheck(status.GetContext(), requiredChecks) {
			continue
		}
		if *status.State == ci.Error || *status.State == ci.Failure {
			if skipContext != nil && skipContext(*status.Context) {
				continue
			}
			log.Printf("%s\t failed", status.GetContext())
			failures++
		} else if *status.State == ci.Pending {
			log.Printf("%s\t pending", status.GetContext())
			pending++
		} else if *status.State == ci.Success {
			log.Printf("%s\t passed", status.GetContext())
			successes++
		} else {
			log.Printf("Check Status %s is unknown", *status.State)
		}
	}
	if pending > 0 {
		return ci.Pending
	} else if failures > 0 {
		return ci.Failure
	} else {
		return ci.Success
	}
}

// IsRequiredCICheck returns true if the check is required to pass before a PR
// can be merged.
// statusCxt is the name of the status
func IsRequiredCICheck(statusCxt string,
	requiredChecks *github.RequiredStatusChecks) bool {
	if requiredChecks == nil {
		return false
	}
	for _, requiredCheckCxt := range requiredChecks.Contexts {
		if statusCxt == requiredCheckCxt {
			return true
		}
	}
	return false
}

// GetAPITokenFromFile returns the github api token from tokenFile
func GetAPITokenFromFile(tokenFile string) (string, error) {
	return GetPasswordFromFile(tokenFile)
}

// GetPasswordFromFile get a string usually is a password or token from a local file
func GetPasswordFromFile(file string) (string, error) {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b[:]))
	if token == "" {
		return "", fmt.Errorf("%s is empty", file)
	}
	return token, nil
}

// CloneRepoCheckoutBranch removes previous repo, clone to local machine,
// change directory into the repo, and checkout the given branch.
// Returns the absolute path to repo root
func CloneRepoCheckoutBranch(gclient *GithubClient, repo, baseBranch, newBranch, pathPrefix string) (string, error) {
	repoPath := path.Join(pathPrefix, repo)
	if err := os.RemoveAll(repoPath); err != nil {
		return "", err
	}
	if pathPrefix != "" {
		if err := os.MkdirAll(pathPrefix, os.FileMode(0755)); err != nil {
			return "", err
		}
		if err := os.Chdir(pathPrefix); err != nil {
			return "", err
		}
	}
	if _, err := ShellSilent(
		"git clone " + gclient.Remote(repo)); err != nil {
		return "", err
	}
	if err := os.Chdir(repo); err != nil {
		return "", err
	}
	if _, err := Shell("git checkout " + baseBranch); err != nil {
		return "", err
	}
	if newBranch != "" {
		if _, err := Shell("git checkout -b " + newBranch); err != nil {
			return "", err
		}
	}
	return os.Getwd()
}

// RemoveLocalRepo deletes the local git repo just cloned
func RemoveLocalRepo(pathToRepo string) error {
	return os.RemoveAll(pathToRepo)
}

// CreateCommitPushToRemote stages call local changes, create a commit,
// and push to remote tracking branch
func CreateCommitPushToRemote(branch, commitMsg string) error {
	// git commit -am does not work with untracked files
	// track new files first and then create a commit
	if _, err := Shell("git add -A"); err != nil {
		return err
	}
	if _, err := Shell("git commit -m " + commitMsg); err != nil {
		return err
	}
	_, err := Shell("git push -f --set-upstream origin " + branch)
	return err
}

// BlockMergingOnBranch adds "do-not-merge/post-submit" labels to all PRs
// in :branch on the repo which the :githubClnt connects to
func BlockMergingOnBranch(githubClnt *GithubClient, repo, branch string) error {
	log.Printf("Adding [%s] label to PRs agaist %s in repo %s", doNotMergeLabel, branch, repo)
	return githubClnt.AddLabelToPRs(github.PullRequestListOptions{
		State: "open",
		Base:  branch,
	}, repo, doNotMergeLabel)
}

// UnBlockMergingOnBranch removes "do-not-merge/post-submit" labels to all PRs
// in :branch on the repo which the :githubClnt connects to
func UnBlockMergingOnBranch(githubClnt *GithubClient, repo, branch string) error {
	log.Printf("Removing [%s] label to PRs agaist %s in repo %s", doNotMergeLabel, branch, repo)
	return githubClnt.RemoveLabelFromPRs(github.PullRequestListOptions{
		State: "open",
		Base:  branch,
	}, repo, doNotMergeLabel)
}
