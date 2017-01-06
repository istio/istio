// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// An example implementation of Echo backend in go.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var (
	port = flag.Int("port", 8080, "default http port")

	mu       sync.Mutex
	requests = 0
	data     = 0
)

func handler(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// echo back the Content-Type and Content-Length in the response
	for _, k := range []string{"Content-Type", "Content-Length"} {
		if v := r.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(http.StatusOK)
	w.Write(body)

	mu.Lock()
	requests++
	data += len(body)
	defer mu.Unlock()
}

func main() {
	flag.Parse()

	go func() {
		for {
			mu.Lock()
			fmt.Printf("Requests Requests: %v  Data: %v\n", requests, data)
			mu.Unlock()
			time.Sleep(time.Second)
		}
	}()

	fmt.Printf("Listening on port %v\n", *port)

	http.HandleFunc("/", handler)
	http.ListenAndServe(":"+strconv.Itoa(*port), nil)
}
