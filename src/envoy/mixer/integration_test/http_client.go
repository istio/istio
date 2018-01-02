// Copyright 2017 Istio Authors. All Rights Reserved.
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
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTP client time out in second.
const HttpTimeOut = 5

// Maximum number of ping the server to wait.
const maxAttempts = 30

// Issue fast request, only care about network error.
// Don't care about server response.
func HTTPFastGet(url string) (err error) {
	client := &http.Client{}
	client.Timeout = time.Duration(HttpTimeOut * time.Second)
	_, err = client.Get(url)
	return err
}

func HTTPGet(url string) (code int, resp_body string, err error) {
	log.Println("HTTP GET", url)
	client := &http.Client{}
	client.Timeout = time.Duration(HttpTimeOut * time.Second)
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
	client.Timeout = time.Duration(HttpTimeOut * time.Second)
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

func ShortLiveHTTPPost(url string, content_type string, req_body string) (code int, resp_body string, err error) {
	log.Println("Short live HTTP POST", url)
	tr := &http.Transport{
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: tr}
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
	client.Timeout = time.Duration(HttpTimeOut * time.Second)
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

func WaitForHttpServer(url string) {
	for i := 0; i < maxAttempts; i++ {
		log.Println("Pinging URL: ", url)
		code, _, err := HTTPGet(url)
		if err == nil && code == http.StatusOK {
			log.Println("Server is up and running...")
			return
		}
		log.Println("Will wait a second and try again.")
		time.Sleep(time.Second)
	}
	log.Println("Give up the wait, continue the test...")
}

func WaitForPort(port int) {
	server_port := fmt.Sprintf("localhost:%v", port)
	for i := 0; i < maxAttempts; i++ {
		log.Println("Pinging port: ", server_port)
		_, err := net.Dial("tcp", server_port)
		if err == nil {
			log.Println("The port is up and running...")
			return
		}
		log.Println("Wait a second and try again.")
		time.Sleep(time.Second)
	}
	log.Println("Give up the wait, continue the test...")
}
