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

// go run githubContrib.go to update Contributions.txt

// This script goes from org -> repos (skipping forks) -> contributors -> user
// -> guess/normalize the company and count contribs

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Check checks for non nil error and dies upon error.
func checkOrDie(err error, msg string) {
	if err != nil {
		log.Fatal(msg, err)
	}
}

// tokenFromEnv gets auth token from the env.
func tokenFromEnv() string {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("Need to have GITHUB_TOKEN set in the env")
	}
	return token
}

// gitHubAPIURL returns the full v3 rest api for a given path.
func gitHubAPIURL(path string) string {
	return "https://api.github.com/" + path
}

// newGhRequest makes a GitHub request (with Accept and Authorization headers).
func newGhRequest(url string) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	checkOrDie(err, "Unable to make request")
	req.Header.Add("Accept", "application/vnd.github.v3+json")
	req.Header.Add("Authorization", "token "+tokenFromEnv())
	req.Header.Add("User-Agent", "githubContribExtractor")
	return req
}

// getBodyForURL gets the body or dies/abort on any error.
func getBodyForURL(url string) []byte {
	req := newGhRequest(url)
	client := &http.Client{}
	resp, err := client.Do(req)
	checkOrDie(err, "Unable to send request")
	body, err := ioutil.ReadAll(resp.Body)
	checkOrDie(err, "Unable to read response")
	succ := resp.StatusCode
	log.Printf("Got %d : %s for %s", succ, resp.Status, url)
	if succ != http.StatusOK {
		os.Exit(1)
	}
	if *debugFlag {
		prettyPrintJSON(body)
	}
	return body
}

// extractResult gets the body as json, parses it or dies/abort on any error.
func extractResult(url string, result interface{}) {
	body := getBodyForURL(url)
	err := json.Unmarshal(body, &result)
	checkOrDie(err, "Unable to parse json")
}

// prettyPrintJSON outputs indented version of the Json body (debug only).
func prettyPrintJSON(body []byte) {
	var out bytes.Buffer
	err := json.Indent(&out, body, "", "  ")
	checkOrDie(err, "Unable to Indent json")
	_, err = out.WriteTo(os.Stdout)
	checkOrDie(err, "Unable to Write json")
}

// repo is what we use from github rest api v3 listing repositories per org.
type repo struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	ContributorsURL string `json:"contributors_url"`
	IsFork          bool   `json:"fork"`
}

// userC is what we care about from what we get from the ContributorsURL.
type userC struct {
	Login         string `json:"login"`
	ID            int64  `json:"id"`
	Contributions int64  `json:"contributions"`
}

// userData is for the json we get from the /users/:username API call.
type userData struct {
	Login   string `json:"login"`
	Name    string `json:"name"` // full name
	Company string `json:"company"`
	Email   string `json:"email"`
}

var fromEmailCount = 0 // global variable ftl (or ftw)

// company returns its best guess of the company for a given GitHub user login.
func company(login string, contribCount int64, user *userData) string {
	extractResult(gitHubAPIURL("users/"+login), user)
	return companyFromUser(*user, contribCount)
}

// Strip stuff in parenthesis, trailing inc and .com or leading @ or stuff after & or second @:
// http://s2.quickmeme.com/img/28/28267ccca83716ccddc3a2e194e8b0052cae3a204de3f37928a20e8ff4f0ee65.jpg
var companyRegex = regexp.MustCompile(`(\(.*\))|([., ]+(com|inc)[ ,.]*$)|( )|(^@)|([&@].*)$`)

func companyFromUser(user userData, contribCount int64) string {
	company := companyRegex.ReplaceAllString(strings.ToLower(user.Company), "")
	if company == "" && user.Email != "" {
		company = companyRegex.ReplaceAllString(strings.ToLower(strings.Split(user.Email, "@")[1]), "")
	}
	// also treat gmail as unknown
	if company != "" && company != "gmail" {
		return strings.ToUpper(company[:1]) + company[1:]
	}
	log.Printf("%s (%s) <%s> has %d contributions but no company nor (useful) email", user.Login, user.Name, user.Email, contribCount)
	return "Unknown"
}

// --- Main --

var debugFlag = flag.Bool("debug", false, "Turn verbose Json debug output")

func main() {
	var minContributions = flag.Int64("min-contributions", 3, "Contributions threshold")
	var orgFlag = flag.String("org", "istio", "Organization to query for repositories")
	var contribFNameFlag = flag.String("output", "Contributions.txt", "Output file name")
	flag.Parse()
	// Get the repos for the org:
	org := *orgFlag
	var repos []repo
	extractResult(gitHubAPIURL("orgs/"+org+"/repos"), &repos)
	log.Printf("%s has %d repos", org, len(repos))
	// For each repo, get populate the user/contrib counts:
	userMap := make(map[string]int64)
	forksCount := 0
	for _, r := range repos {
		if r.IsFork {
			log.Printf("Skipping %s which is a fork", r.Name)
			forksCount++
			continue
		}
		var users []userC
		extractResult(r.ContributorsURL, &users)
		for _, u := range users {
			userMap[u.Login] += u.Contributions
		}
	}
	log.Printf("%s has %d forks", org, forksCount)
	skippedUsers := 0
	contributors := 0
	// Contributor and contributions count by company
	type coCounts struct {
		contributors  int
		contributions int64
	}
	companiesMap := make(map[string]coCounts)
	for login, c := range userMap {
		if c >= *minContributions {
			contributors++
			var user userData
			company := company(login, c, &user)
			fmt.Printf("user %d %+v %s\n", c, user, company)
			// yuck! why is that tmp needed... because https://github.com/golang/go/issues/3117
			var tmp = companiesMap[company]
			tmp.contributors++
			tmp.contributions += c
			companiesMap[company] = tmp
		} else {
			skippedUsers++
		}
	}
	log.Printf("%d contributors + %d users skipped because they have less than %d contributions",
		contributors, skippedUsers, *minContributions)
	log.Printf("%d companies found, %d guessed from email", len(companiesMap), fromEmailCount)
	// stdout full data:
	for co, counts := range companiesMap {
		fmt.Printf("company %s %d contributors totaling %d contributions\n", co, counts.contributors, counts.contributions)
	}
	// Update the file whose content is shown in FAQ entry:
	contributionsFileName := *contribFNameFlag
	log.Printf("Updating %s (to be committed/git pushed)", contributionsFileName)
	sortedCos := make([]string, 0, len(companiesMap))
	for co := range companiesMap {
		sortedCos = append(sortedCos, co)
	}
	sort.Strings(sortedCos)

	out, err := os.Create(contributionsFileName)
	checkOrDie(err, "unable to create/open "+contributionsFileName)
	t := time.Now()
	y, mon, _ := t.Date()
	_, err = fmt.Fprintf(out, "Here is the current (as of %s %d) alphabetical list of companies and the number of contributors:\n", mon.String(), y)
	checkOrDie(err, contributionsFileName)
	first := true
	for _, co := range sortedCos {
		if !first {
			_, err = fmt.Fprint(out, ", ")
			checkOrDie(err, contributionsFileName)
		} else {
			first = false
		}
		_, err = fmt.Fprintf(out, "%s (%d)", co, companiesMap[co].contributors)
		checkOrDie(err, contributionsFileName)
	}
	_, err = fmt.Fprintf(out, "\n")
	checkOrDie(err, contributionsFileName)
	log.Printf("All done ! Double check %s\n", contributionsFileName)
}
