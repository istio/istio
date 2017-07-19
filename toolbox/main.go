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
	"time"

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
	remoteURL, err := remote(dep.repoName)
	if err != nil {
		return line, err
	}
	latestStableCommit, err := util.Shell(
		fmt.Sprintf("git ls-remote %s %s", remoteURL, dep.prodBranch))
	if err != nil {
		return line, err
	}
	commitSHA := latestStableCommit[:strings.Index(latestStableCommit, "\t")]
	idx := strings.Index(line, "\"")
	return line[:idx] + "\"" + commitSHA + "\",", nil
}

// Get the list of dependencies of a repo
func getDeps(repo string) []dependency {
	// TODO (chx) read from file in each repo
	// TODO (chx) comment each function
	return deps[repo]
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
	branch := fmt.Sprintf("autoUpdateDeps%v", time.Now().UnixNano())
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
	if _, err := util.Shell("git push -f --set-upstream origin " + branch); err != nil {
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
	buildDepsGraph()
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
