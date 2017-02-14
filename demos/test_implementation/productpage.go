// Copyright 2016 IBM Corporation
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

const (
	userCookie      = "user"
	requestIDHeader = "X-Request-ID"
)

type productPage struct {
	Details map[string]string  `json:"details,omitempty"`
	Reviews map[string]*review `json:"reviews,omitempty"`
}

type review struct {
	Text   string  `json:"text,omitempty"`
	Rating *rating `json:"rating,omitempty"`
}

type rating struct {
	Stars int    `json:"stars,omitempty"`
	Color string `json:"color,omitempty"`
}

func main() {
	port := "9080"
	http.HandleFunc("/productpage", productpageHandler)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func productpageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/json")

	headers := getForwardHeaders(r)
	details := getDetails(headers)
	reviews := getReviews(headers)

	page := productPage{
		Details: details,
		Reviews: reviews,
	}
	bytes, _ := json.Marshal(page)

	w.Write(bytes)
}

func getDetails(forwardHeaders http.Header) map[string]string {
	const attempts = 1
	const timeout = 1 * time.Second

	bytes, err := doRequest("http://details.default.svc:9080/details", forwardHeaders, timeout, attempts)
	if err != nil {
		log.Printf("Error getting details: %v", err)
		return nil
	}

	details := map[string]string{}
	err = json.Unmarshal(bytes, &details)
	if err != nil {
		log.Printf("Error unmarshaling details: %v", err)
		return nil
	}

	return details
}

func getReviews(forwardHeaders http.Header) map[string]*review {
	const attempts = 2
	const timeout = 3 * time.Second

	bytes, err := doRequest("http://reviews.default.svc:9080/reviews", forwardHeaders, timeout, attempts)
	if err != nil {
		log.Printf("Error getting reviews: %v", err)
		return nil
	}

	reviews := map[string]*review{}
	err = json.Unmarshal(bytes, &reviews)
	if err != nil {
		log.Printf("Error unmarshaling reviews: %v", err)
		return nil
	}

	return reviews
}

func doRequest(url string, forwardHeaders http.Header, timeout time.Duration, attempts int) ([]byte, error) {
	client := http.Client{}
	client.Timeout = timeout

	for i := 0; i < attempts; i++ {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header = forwardHeaders

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error executing HTTP request (%s %s): %v", req.Method, req.URL, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			log.Printf("Error executing HTTP request (%s %s): received status code %d", req.Method, req.URL, resp.StatusCode)
			continue
		}

		bytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading HTTP response (%s %s): %v", req.Method, req.URL, err)
			continue
		}

		return bytes, nil
	}

	err := fmt.Errorf("run out of attempts")
	log.Printf("Error executing HTTP request (%s %s): %v", "GET", url, err)
	return nil, err
}

func getForwardHeaders(r *http.Request) http.Header {
	fwdReq, _ := http.NewRequest("GET", "dummy", nil)

	cookie, err := r.Cookie(userCookie)
	if err != http.ErrNoCookie {
		fwdReq.AddCookie(cookie)
	}

	reqID := r.Header.Get(requestIDHeader)
	if reqID != "" {
		fwdReq.Header.Set(requestIDHeader, reqID)
	}

	return fwdReq.Header
}
