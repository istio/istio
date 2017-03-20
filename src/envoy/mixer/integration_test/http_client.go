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

package test

import (
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
)

func HTTPGet(url string) (code int, resp_body string, err error) {
	log.Println("HTTP GET", url)
	client := &http.Client{}
	resp, err := client.Get(url)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	resp_body = string(body)
	code = resp.StatusCode
	log.Println(resp_body)
	return code, resp_body, nil
}

func HTTPPost(url string, content_type string, req_body string) (code int, resp_body string, err error) {
	log.Println("HTTP POST", url)
	client := &http.Client{}
	resp, err := client.Post(url, content_type, strings.NewReader(req_body))
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	resp_body = string(body)
	code = resp.StatusCode
	log.Println(resp_body)
	return code, resp_body, nil
}

func HTTPGetWithHeaders(l string, headers map[string]string) (code int, resp_body string, err error) {
	log.Println("HTTP GET with headers: ", l)
	client := &http.Client{}
	req := http.Request{}

	req.Header = map[string][]string{}
	for k, v := range headers {
		req.Header[k] = []string{v}
	}
	req.Method = http.MethodGet
	req.URL, _ = url.Parse(l)

	resp, err := client.Do(&req)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	resp_body = string(body)
	code = resp.StatusCode
	log.Println(resp_body)
	return code, resp_body, nil
}
