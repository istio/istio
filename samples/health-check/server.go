// Copyright Istio Authors
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
	"net/http"
)

func main() {
	finish := make(chan bool)
	server8001 := http.NewServeMux()
	server8001.HandleFunc("/foo", foo8001)
	server8001.HandleFunc("/bar", bar8001)

	server8002 := http.NewServeMux()
	server8002.HandleFunc("/foo", foo8002)
	server8002.HandleFunc("/bar", bar8002)

	go func() {
		http.ListenAndServe(":8001", server8001)
	}()

	go func() {
		http.ListenAndServe(":8002", server8002)
	}()

	<-finish
}

func foo8001(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("Listening on 8001: foo "))
}

func bar8001(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("Listening on 8001: bar "))
}

func foo8002(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("Listening on 8002: foo "))
}

func bar8002(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("Listening on 8002: bar "))
}
