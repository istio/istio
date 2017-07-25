// Copyright 2017 Istio Authors
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

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"istio.io/istio/tests/e2e/util"
)

var (
	repo       = flag.String("repo", "", "Update dependencies of only this repository")
	githubClnt *githubClient
)

// Generates the url to the remote repository on github
// embeded with proper username and token
func remote(repo string) (string, error) {
	acct := *owner // passed in as flag in githubClient
	token, err := getToken()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s:%s@github.com/%s/%s.git", acct, token, acct, repo), nil
}

// Update the commit SHA reference in a given line from dependency file
// to the latest stable version
// returns the updated line
func replaceCommit(line string, dep dependency) (string, error) {
	commitSHA, err := githubClnt.getHeadCommitSHA(dep.repoName, dep.prodBranch)
	if err != nil {
		return line, err
	}
	idx := strings.Index(line, "\"")
	return line[:idx] + "\"" + commitSHA + "\",", nil
}

// Get the list of dependencies of a repo
func getDeps(repo string) []dependency {
	// TODO (chx) implement the dependency hoisting in all repos
	// TODO (chx) read dependencies from file in each parent repo
	deps := make(map[string][]dependency)
	deps["mixerclient"] = []dependency{
		{"mixerapi_git", "api", "master", "repositories.bzl"},
	}
	deps["galley"] = []dependency{
		{"com_github_istio_api", "api", "master", "WORKSPACE"},
	}

	deps["mixer"] = []dependency{
		{"com_github_istio_api", "api", "master", "WORKSPACE"},
	}

	deps["proxy"] = []dependency{
		{"mixerclient_git", "mixerclient", "master", "src/envoy/mixer/repositories.bzl"},
	}

	deps["pilot"] = []dependency{
		{"PROXY", "proxy", "stable", "WORKSPACE"},
	}
	return deps[repo]
}

// Generates an MD5 digest of the version set of the repo dependencies
// useful in avoiding making duplicate branches of the same code change
func fingerPrint(repo string) (string, error) {
	digest := ""
	for _, dep := range getDeps(repo) {
		commitSHA, err := githubClnt.getHeadCommitSHA(dep.repoName, dep.prodBranch)
		if err != nil {
			return "", err
		}
		digest = digest + commitSHA
	}
	return util.GetMD5Hash(digest), nil
}

// Update the commit SHA reference in the dependency file of dep
func updateDepFile(dep dependency) error {
	input, err := ioutil.ReadFile(dep.file)
	if err != nil {
		return err
	}
	lines := strings.Split(string(input), "\n")
	for i, line := range lines {
		if strings.Contains(line, dep.name+" = ") {
			if lines[i], err = replaceCommit(line, dep); err != nil {
				return err
			}
		}
	}
	output := strings.Join(lines, "\n")
	return ioutil.WriteFile(dep.file, []byte(output), 0600)
}

// Update the given repository so that it uses the latest dependency references
// push new branch to remote, create pull request on master,
// which is auto-merged after presumbit
func updateDeps(repo string) error {
	if _, err := util.Shell("rm -rf " + repo); err != nil {
		return err
	}
	remoteURL, err := remote(repo)
	if err != nil {
		return err
	}
	if _, err := util.Shell("git clone " + remoteURL); err != nil {
		return err
	}
	if err := os.Chdir(repo); err != nil {
		return err
	}
	depVersions, err := fingerPrint(repo)
	if err != nil {
		return err
	}
	branch := "autoUpdateDeps" + depVersions
	if _, err := util.Shell("git checkout -b " + branch); err != nil {
		return err
	}
	for _, dep := range getDeps(repo) {
		if err := updateDepFile(dep); err != nil {
			return err
		}
	}
	if _, err := util.Shell("git add *"); err != nil {
		return err
	}
	if _, err := util.Shell("git commit -m Update_Dependencies"); err != nil {
		return err
	}
	if _, err := util.Shell("git push --set-upstream origin " + branch); err != nil {
		return err
	}
	if err := githubClnt.createPullRequest(branch, repo); err != nil {
		return err
	}
	if err := os.Chdir(".."); err != nil {
		return err
	}
	if _, err := util.Shell("rm -rf " + repo); err != nil {
		return err
	}
	return nil
}

func main() {
	flag.Parse()
	var err error
	githubClnt, err = newGithubClient()
	if err != nil {
		log.Panicf("Error when initializing github client: %v\n", err)
	}
	if *repo != "" { // only update dependencies of this repo
		if err := updateDeps(*repo); err != nil {
			log.Panicf("Failed to udpate dependency: %v\n", err)
		}
	} else { // update dependencies of all repos in the istio project
		repos, err := githubClnt.getListRepos()
		if err != nil {
			log.Panicf("Error when fetching list of repos: %v\n", err)
			return
		}
		for _, r := range repos {
			if err := updateDeps(r); err != nil {
				log.Panicf("Failed to udpate dependency: %v\n", err)
			}
		}
	}
}
