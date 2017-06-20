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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// getToken gets auth token from the env
func getToken() string {
	// fmt.Println("in GetToken")
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("Need to have GITHUB_TOKEN set in the env")
	}
	return token
}

var gAuthToken = getToken() // Any other way to get a static ?

// GitHubAPIURL returns the full v3 rest api for a given path
func GitHubAPIURL(path string) string {
	return "https://api.github.com/" + path
}

// GetBodyForURL gets the body or dies/abort on any error
func GetBodyForURL(url string) []byte {
	req := NewGhRequest(url)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Unable to send request", err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Unable to read response", err)
	}
	succ := resp.StatusCode
	log.Printf("Got %d : %s for %s", succ, resp.Status, url)
	if succ != http.StatusOK {
		os.Exit(1)
	}
	return body
}

// NewGhRequest makes a GitHub request (with Accept and Authorization headers)
func NewGhRequest(url string) *http.Request {
	// fmt.Println("in GhRequest")
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Add("Accept", "application/vnd.github.v3+json")
	req.Header.Add("Authorization", "token "+gAuthToken)
	req.Header.Add("User-Agent", "githubContribExtractor")
	if err != nil {
		log.Fatal("Unable to make request", err)
	}
	return req
}

// PrettyPrintJSON outputs indented version of the Json body
func PrettyPrintJSON(body []byte) {
	var out bytes.Buffer
	err := json.Indent(&out, body, "", "  ")
	if err != nil {
		log.Fatal("Unable to Indent json", err)
	}
	_, err = os.Stdout.Write(out.Bytes())
	if err != nil {
		log.Fatal("Unable to output json", err)
	}
}

// Repo is what we use from github rest api v3 listing repositories per org
type Repo struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	ContributorsURL string `json:"contributors_url"`
	IsFork          bool   `json:"fork"`
}

// UserC is what we care about from what we get from the ContributorsURL
type UserC struct {
	Login         string `json:"login"`
	ID            int64  `json:"id"`
	Contributions int64  `json:"contributions"`
}

// UserData is for the json we get from the /users/:username API call
type UserData struct {
	Name    string `json:"name"` // full name
	Company string `json:"company"`
	Email   string `json:"email"`
}

var fromEmailCount = 0 // global variable ftl (or ftw)

// RemoveFromEnd returns a string with the suffix
func RemoveFromEnd(s string, what string) string {
	if !strings.HasSuffix(s, what) {
		return s
	}
	return s[:len(s)-len(what)]
}

// GetCompany returns it's best guess of the company for a given GitHub user login
func GetCompany(login string, contribCount int64) string {
	url := GitHubAPIURL("users/" + login)
	body := GetBodyForURL(url)
	var userData UserData
	err := json.Unmarshal(body, &userData)
	if err != nil {
		log.Fatal("Unable to parse json", err)
	}
	var company string
	if userData.Company != "" {
		company = strings.Replace(userData.Company, " ", "", -1)
	} else if userData.Email != "" {
		fromEmailCount++
		company = strings.Split(userData.Email, "@")[1]
	}
	// Many people have an @ and/or trailing spaces:
	company = strings.ToLower(strings.TrimLeft(strings.TrimSpace(company), "@"))
	company = RemoveFromEnd(company, ".com")
	company = RemoveFromEnd(company, ".")
	company = RemoveFromEnd(company, "inc")
	company = RemoveFromEnd(company, ",")
	company = RemoveFromEnd(company, ".")
	// also treat gmail as unknown
	if company != "" && company != "gmail" {
		return strings.ToUpper(company[:1]) + company[1:]
	}
	log.Printf("%s (%s) has %d contributions but no company nor email", login, userData.Name, contribCount)
	return "Unknown"
}

// --- Main --

const minContributions = 3
const debugJSON = false

func main() {
	// fmt.Println("in main")
	const org = "istio"
	url := GitHubAPIURL("orgs/" + org + "/repos")
	body := GetBodyForURL(url)
	var repos []Repo
	err := json.Unmarshal(body, &repos)
	if err != nil {
		log.Fatal("Unable to parse json", err)
	}
	log.Printf("%s has %d repos", org, len(repos))
	if debugJSON {
		PrettyPrintJSON(body)
	}
	// For each repo, get populate the user/contrib counts:
	userMap := make(map[string]int64)
	forksCount := 0
	for _, r := range repos {
		if r.IsFork {
			log.Printf("Skipping %s which is a fork", r.Name)
			forksCount++
			continue
		}
		body := GetBodyForURL(r.ContributorsURL)
		//PrettyPrintJSON(body)
		var users []UserC
		err = json.Unmarshal(body, &users)
		if err != nil {
			log.Fatal("Unable to parse json", err)
		}
		for _, u := range users {
			userMap[u.Login] += u.Contributions
		}
	}
	log.Printf("%s has %d forks", org, forksCount)
	skippedUsers := 0
	contributors := 0
	// contributor count by company
	type CoCounts struct {
		contributors  int
		contributions int64
	}
	// tried a struct as value and single map and failed...
	companiesMap := make(map[string]CoCounts)
	for u, c := range userMap {
		if c >= minContributions {
			contributors++
			fmt.Printf("user %d %s\n", c, u)
			company := GetCompany(u, c)
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
		contributors, skippedUsers, minContributions)
	log.Printf("%d companies found, %d guessed from email", len(companiesMap), fromEmailCount)
	for co, counts := range companiesMap {
		fmt.Printf("company %s %d contributors totaling %d contributions\n", co, counts.contributors, counts.contributions)
	}
	const contributionsFileName = "Contributions.txt"
	log.Printf("Updating %s (to be committed/git pushed)", contributionsFileName)
	var sortedCos []string
	for co := range companiesMap {
		sortedCos = append(sortedCos, co)
	}
	sort.Strings(sortedCos)

	out, err := os.Create(contributionsFileName)
	if err != nil {
		log.Fatal("unable to create/open", contributionsFileName, err)
	}
	first := true
	for _, co := range sortedCos {
		if !first {
			fmt.Fprint(out, ", ")
		} else {
			t := time.Now()
			y, mon, _ := t.Date()
			fmt.Fprintf(out, "Here is the current (as of %s %d) alphabetical list of companies and the number of contributors:\n", mon.String(), y)
			first = false
		}
		fmt.Fprintf(out, "%s (%d)", co, companiesMap[co].contributors)
	}
	fmt.Fprintf(out, "\n")
	log.Printf("All done ! Double check %s\n", contributionsFileName)
}
