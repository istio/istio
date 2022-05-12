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
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"regexp"

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

type Handler struct {
	// Mapping table for support conversion of tag.
	// The key is combination of the image name and tag with `:` separator,
	// and the value is the target tag or sha.
	// For example,
	//  1) Convert from a tag to a digest
	//     istio-testing/awesomedocker:latest -> sha256:abcedf0123456789
	//  2) Convert from a tag to another tag
	//     istio-testing/awesomedocker:v1.0.0 -> v1.0.1
	tagMap map[string]string
}

var re = regexp.MustCompile(`(?P<Prefix>/v\d+)?/(?P<ImageName>.+)/manifests/(?P<Tag>[^:]*)$`)

// Convert tag based on the tag map.
// If the given path does not have tagged form or the image name and tag is not the registered one,
// just return the path without modification.
func (h *Handler) convertTag(path string) string {
	matches := re.FindStringSubmatch(path)
	if matches == nil {
		return path
	}

	prefix := matches[re.SubexpIndex("Prefix")]
	imageName := matches[re.SubexpIndex("ImageName")]
	tag := matches[re.SubexpIndex("Tag")]
	key := imageName + ":" + tag

	log.Infof("key: %v", key)

	if converted, found := h.tagMap[imageName+":"+tag]; found {
		return prefix + "/" + imageName + "/manifests/" + converted
	}
	return path
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ready" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// convert the requested tag to the other tag or sha of the real registry
	if r.URL.Path == "/admin/v1/tagmap" {
		switch r.Method {
		case http.MethodPost:
			m := map[string]string{}
			err := json.NewDecoder(r.Body).Decode(&m)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			h.tagMap = m
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if jsEncodedMap, err := json.Marshal(h.tagMap); err == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, "%s", jsEncodedMap)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
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
	rurl := fmt.Sprintf("https://%v%v", *registry, h.convertTag(r.URL.Path))
	log.Infof("Get %q, send redirect to %q", r.URL, rurl)
	http.Redirect(w, r, rurl, http.StatusMovedPermanently)
}

func main() {
	flag.Parse()
	s := &http.Server{
		Addr: fmt.Sprintf(":%d", *port),
		Handler: &Handler{
			tagMap: make(map[string]string),
		},
	}
	log.Infof("registryredirector server is starting at %d", *port)
	if err := s.ListenAndServe(); err != nil {
		log.Error(err)
	}
}
