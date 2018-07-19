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

package env

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"istio.io/fortio/fhttp"
)

// HTTP client time out in second.
const httpTimeOut = 5

// Maximum number of ping the server to wait.
const maxAttempts = 30

// HTTPFastGet only cares about network error.
// Issue fast request, only care about network error.
// Don't care about server response.
func HTTPFastGet(url string) (err error) {
	client := &http.Client{}
	client.Timeout = httpTimeOut * time.Second
	_, err = client.Get(url)
	return err
}

// HTTPGet send GET
func HTTPGet(url string) (code int, respBody string, err error) {
	log.Println("HTTP GET", url)
	client := &http.Client{}
	client.Timeout = httpTimeOut * time.Second
	resp, err := client.Get(url)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	respBody = string(body)
	code = resp.StatusCode
	log.Println(respBody)
	return code, respBody, nil
}

// HTTPPost sends POST
func HTTPPost(url string, contentType string, reqBody string) (code int, respBody string, err error) {
	log.Println("HTTP POST", url)
	client := &http.Client{}
	client.Timeout = httpTimeOut * time.Second
	resp, err := client.Post(url, contentType, strings.NewReader(reqBody))
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	respBody = string(body)
	code = resp.StatusCode
	log.Println(fhttp.DebugSummary(body, 512))
	return code, respBody, nil
}

// ShortLiveHTTPPost send HTTP without keepalive
func ShortLiveHTTPPost(url string, contentType string, reqBody string) (code int, respBody string, err error) {
	log.Println("Short live HTTP POST", url)
	tr := &http.Transport{
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Post(url, contentType, strings.NewReader(reqBody))
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	respBody = string(body)
	code = resp.StatusCode
	log.Println(respBody)
	return code, respBody, nil
}

// HTTPGetWithHeaders send HTTP with headers
func HTTPGetWithHeaders(l string, headers map[string]string) (code int, respBody string, err error) {
	log.Println("HTTP GET with headers: ", l)
	client := &http.Client{}
	client.Timeout = httpTimeOut * time.Second
	req := http.Request{}

	req.Header = map[string][]string{}
	for k, v := range headers {
		req.Header[k] = []string{v}
	}
	req.Method = http.MethodGet
	req.URL, err = url.Parse(l)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}

	resp, err := client.Do(&req)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return 0, "", err
	}
	respBody = string(body)
	code = resp.StatusCode
	log.Println(respBody)
	return code, respBody, nil
}

// WaitForHTTPServer waits for a HTTP server
func WaitForHTTPServer(url string) {
	for i := 0; i < maxAttempts; i++ {
		log.Println("Pinging URL: ", url)
		code, _, err := HTTPGet(url)
		if err == nil && code == http.StatusOK {
			log.Println("Server is up and running...")
			return
		}
		log.Println("Will wait a second and try again.")
		time.Sleep(200 * time.Millisecond)
	}
	log.Println("Give up the wait, continue the test...")
}

// WaitForPort waits for a TCP port
func WaitForPort(port uint16) {
	serverPort := fmt.Sprintf("localhost:%v", port)
	for i := 0; i < maxAttempts; i++ {
		log.Println("Pinging port: ", serverPort)
		_, err := net.Dial("tcp", serverPort)
		if err == nil {
			log.Println("The port is up and running...")
			return
		}
		log.Println("Wait a second and try again.")
		time.Sleep(200 * time.Millisecond)
	}
	log.Println("Give up the wait, continue the test...")
}

// IsPortUsed checks if a port is used
func IsPortUsed(port uint16) bool {
	serverPort := fmt.Sprintf("localhost:%v", port)
	_, err := net.Dial("tcp", serverPort)
	return err == nil
}
