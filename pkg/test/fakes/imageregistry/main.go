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
	"crypto/tls"
	"encoding/base64"
	"flag"
	"fmt"
	"net/http"
	"strings"

	"istio.io/pkg/log"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	containerregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

var port = flag.Int("port", 1338, "port to run registry on")
var images = flag.String("images", "", "comma separated list of images that will be preloaded and served")
var registry = flag.String("registry", "localhost:1338", "name of registry to push and pull image")

const (
	User   = "user"
	Passwd = "passwd"
)

type Handler struct {
	registry http.Handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	encoded := base64.StdEncoding.EncodeToString([]byte("user:passwd"))
	authHdr := r.Header.Get("Authorization")
	wantHdr := fmt.Sprintf("Basic %s", encoded)
	if authHdr != wantHdr {
		log.Infof("Unauthorized: " + r.URL.Path)
		log.Infof("Got header %v want header %v", authHdr, wantHdr)
		w.Header().Set("WWW-Authenticate", "Basic")
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	h.registry.ServeHTTP(w, r)
}

func main() {
	flag.Parse()
	s := &http.Server{
		Addr: fmt.Sprintf(":%d", *port),
		Handler: &Handler{
			registry: containerregistry.New(),
		},
	}
	done := make(chan bool, 1)
	go func() {
		if err := s.ListenAndServe(); err != nil {
			log.Error(err)
		}
		done <- true
	}()
	parts := strings.Split(*images, ",")
	for _, originImg := range parts {
		if originImg == "" {
			continue
		}
		log.Infof("Pulling image %v", originImg)
		// Get the new image name from the original image.
		originImageParts := strings.Split(originImg, "/")
		originImageParts[0] = *registry
		newImage := strings.Join(originImageParts, "/")

		// Pull the origin image.
		ref, err := name.ParseReference(originImg)
		if err != nil {
			log.Errorf("Could not parse url in image reference: %v", err)
			continue
		}
		t := remote.DefaultTransport.Clone()
		t.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint: gosec
		}
		img, err := remote.Image(ref, remote.WithTransport(t))
		if err != nil {
			log.Errorf("Could not fetch image %v", err)
			continue
		}

		log.Infof("Pusing image %v", newImage)
		// Push the image with new tag.
		tag, err := name.ParseReference(newImage)
		options := []remote.Option{
			remote.WithAuth(&authn.Basic{Username: User, Password: Passwd}),
		}
		if err := remote.Write(tag, img, options...); err != nil {
			log.Errorf("Failed to push image %v: %v", tag, err)
		}
	}
	<-done
}
