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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

// Accept: application/vnd.github.v3+json
func main() {
	token := os.Getenv("GITHUB_TOKEN")
	fmt.Printf("Github %d.\n", len(token))
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://api.github.com/orgs/istio/repos", nil)
	req.Header.Add("Accept", "application/vnd.github.v3+json")
	// req.Header.Add()// auth
	if err != nil {
		log.Fatal("Unable to make request", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Unable to send request", err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Unable to read response", err)
	}
	succ := resp.StatusCode
	log.Printf("Got %d : %s", succ, resp.Status)
	if succ != http.StatusOK {
		os.Exit(1)
	}
	//	fmt.Printf("Got %d\n%s\n", succ, body)
	//body = []byte("[{\"id\": 78482929,\"name\": \"test-infra\",\"full_name\": \"istio/test-infra\"},{\"id\": 78482930,\"name\": \"blah-blah\",\"full_name\": \"istio/blah-blah\"}]")
	//var f interface{}

	type Repo struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		/*
			contributors_url string
		*/
	}

	var r []Repo

	err = json.Unmarshal(body, &r)
	if err != nil {
		log.Fatal("Unable to parse json", err)
	}
	for i, v := range r {
		fmt.Println(i)
		fmt.Println(v)
	}

	/*
		var out bytes.Buffer
		err = json.Indent(&out, body, "", "  ")
		if err != nil {
			log.Fatal("Unable to Indent json", err)
		}
		fmt.Printf("%s", out.Bytes())
	*/
	//json.Indent(dst, src, prefix, indent)
	/*
		m := f.(map[string]interface{})
		for k, v := range m {
			switch vv := v.(type) {
			case string:
				fmt.Println(k, "is string", vv)
			case int:
				fmt.Println(k, "is int", vv)
			case []interface{}:
				fmt.Println(k, "is an array:")
				for i, u := range vv {
					fmt.Println(i, u)
				}
			default:
				fmt.Println(k, "is of a type I don't know how to handle")
			}
		}
	*/
}
