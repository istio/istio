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
	"encoding/base64"
	"flag"
	"fmt"
	"net/http"

	"istio.io/pkg/log"
)

var (
	port     = flag.Int("port", 1338, "port to run registry on")
	registry = flag.String("registry", "gcr.io", "name of registry to redirect registry request to")
)

const (
	User   = "user"
	Passwd = "passwd"
)

type Handler struct{}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ready" {
		w.WriteHeader(http.StatusOK)
		return
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%v:%v", User, Passwd)))
	authHdr := r.Header.Get("Authorization")
	wantHdr := fmt.Sprintf("Basic %s", encoded)
	if authHdr != wantHdr {
		log.Infof("Unauthorized: " + r.URL.Path)
		log.Infof("Got header %q want header %q", authHdr, wantHdr)
		w.Header().Set("WWW-Authenticate", "Basic")
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	rurl := fmt.Sprintf("https://%v%v", *registry, r.URL.Path)
	log.Infof("Get %q, send redirect to %q", r.URL, rurl)
	http.Redirect(w, r, rurl, http.StatusMovedPermanently)
}

func main() {
	flag.Parse()
	s := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: &Handler{},
	}
	if err := s.ListenAndServe(); err != nil {
		log.Error(err)
	}
}
