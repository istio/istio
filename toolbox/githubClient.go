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
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

var (
	tokenFile = flag.String("token_file", "", "File containing Auth Token.")
	owner     = flag.String("owner", "istio-testing", "Github Owner or org.")
	token     = ""
)

type githubClient struct {
	client *github.Client
}

// Get github api token from tokenFile
func getToken() (string, error) {
	if token != "" {
		return token, nil
	}
	if *tokenFile == "" {
		return "", fmt.Errorf("token_file not provided")
	}
	b, err := ioutil.ReadFile(*tokenFile)
	if err != nil {
		return "", err
	}
	token = strings.TrimSpace(string(b[:]))
	return token, nil
}

func newGithubClient() (*githubClient, error) {
	token, err := getToken()
	if err != nil {
		return nil, err
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	if err != nil {
		return nil, err
	}
	client := github.NewClient(tc)
	return &githubClient{client}, nil
}

func (g githubClient) createPullRequest(branch, repo string) error {
	if branch == "" {
		return errors.New("branch cannot be empty")
	}
	title := fmt.Sprintf("[DO NOT MERGE] Auto PR to update dependencies of %s", repo)
	body := "This PR will be merged automatically once checks are successful."
	base := "master"
	req := github.NewPullRequest{
		Head:  &branch,
		Base:  &base,
		Title: &title,
		Body:  &body,
	}
	log.Printf("Creating a PR with Title: \"%s\" for repo %s", title, repo)
	pr, _, err := g.client.PullRequests.Create(context.TODO(), *owner, repo, &req)
	if err != nil {
		return err
	}
	log.Printf("Created new PR at %s", *pr.HTMLURL)
	return nil
}

func (g githubClient) getListRepos() ([]string, error) {
	opt := &github.RepositoryListOptions{Type: "owner"}
	repos, _, err := g.client.Repositories.List(context.Background(), *owner, opt)
	if err != nil {
		return nil, err
	}
	var listRepoNames []string
	for _, r := range repos {
		listRepoNames = append(listRepoNames, *r.Name)
	}
	return listRepoNames, nil
}

func (g githubClient) getHeadCommitSHA(repo, branch string) (string, error) {
	ref, _, err := g.client.Git.GetRef(context.Background(), *owner, repo, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	return *ref.Object.SHA, nil
}
