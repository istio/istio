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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"istio.io/istio/pkg/log"
)

var (
	port             = flag.Int("port", 1338, "port to run registry on")
	registry         = flag.String("registry", "gcr.io", "name of registry to redirect registry request to")
	scheme           = flag.String("scheme", "https", "scheme of the URL to the image registry")
	regexForManifest = regexp.MustCompile(`(?P<Prefix>/v\d+)?/(?P<ImageName>.+)/manifests/(?P<Tag>[^:]*)$`)
	regexForLayer    = regexp.MustCompile(`/layer/v1/(?P<ImageName>[^:]+):(?P<Tag>[^:]+)`)
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

// Convert tag based on the tag map.
// If the given path does not have tagged form or the image name and tag is not the registered one,
// just return the path without modification.
func (h *Handler) convertTag(path string) string {
	matches := regexForManifest.FindStringSubmatch(path)
	if matches == nil {
		return path
	}

	prefix := matches[regexForManifest.SubexpIndex("Prefix")]
	imageName := matches[regexForManifest.SubexpIndex("ImageName")]
	tag := matches[regexForManifest.SubexpIndex("Tag")]
	key := imageName + ":" + tag

	log.Infof("key: %v", key)

	if converted, found := h.tagMap[imageName+":"+tag]; found {
		return prefix + "/" + imageName + "/manifests/" + converted
	}
	return path
}

// getFirstLayerURL returns the URL for the first layer of the given image.
// `tag` will be converted by using `tagMap`
func (h *Handler) getFirstLayerURL(imageName string, tag string) (string, error) {
	convertedTag := tag
	if converted, found := h.tagMap[imageName+":"+tag]; found {
		convertedTag = converted
	}

	u := fmt.Sprintf("%v/%v:%v", *registry, imageName, convertedTag)
	var opts []name.Option
	if *scheme == "http" {
		opts = append(opts, name.Insecure)
	}
	ref, err := name.ParseReference(u, opts...)
	if err != nil {
		return "", fmt.Errorf("could not parse url in image reference: %v", err)
	}

	t := remote.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // nolint: gosec // test only code
	desc, err := remote.Get(ref, remote.WithTransport(t))
	if err != nil {
		return "", fmt.Errorf("could not get the description: %v", err)
	}

	manifest, err := partial.Manifest(desc)
	if err != nil {
		return "", fmt.Errorf("failed to get manifest: %v", err)
	}
	if len(manifest.Layers) != 1 {
		return "", fmt.Errorf("docker image does not have one layer (got %v)", len(manifest.Layers))
	}

	return fmt.Sprintf("%s://%v/v2/%v/blobs/%v", *scheme, *registry, imageName, manifest.Layers[0].Digest.String()), nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	insecureClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // nolint: gosec // test only code
			},
		},
	}

	switch p := r.URL.Path; {
	case p == "/ready":
		w.WriteHeader(http.StatusOK)
	case p == "/admin/v1/tagmap":
		// convert the requested tag to the other tag or sha of the real registry
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
	case strings.HasPrefix(p, "/layer/v1/"):
		// returns the blob URL of the first layer in the given OCI image
		// URL path would have the form of /layer/v1/<image name>:<tag>
		matches := regexForLayer.FindStringSubmatch(p)
		if matches == nil {
			http.Error(w, fmt.Sprintf("Malformed URL Path: %q", p), http.StatusBadRequest)
			return
		}
		imageName := matches[regexForLayer.SubexpIndex("ImageName")]
		tag := matches[regexForLayer.SubexpIndex("Tag")]
		rurl, err := h.getFirstLayerURL(imageName, tag)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Infof("Get %q, send redirect to %q", r.URL, rurl)
		http.Redirect(w, r, rurl, http.StatusMovedPermanently)
	case !strings.Contains(p, "/v2/") || !strings.Contains(p, "/blobs/"):
		// only requires authentication for getting manifests, not blobs
		authHdr := r.Header.Get("Authorization")
		if authHdr == "" {
			log.Infof("Unauthorized: " + r.URL.Path)
			log.Infof("Got empty header: %q", authHdr)
			w.Header().Set("WWW-Authenticate", "Basic")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		fallthrough
	default:
		targetURL := fmt.Sprintf("%s://%v%v", *scheme, *registry, h.convertTag(r.URL.Path))
		req, err := http.NewRequest(http.MethodGet, targetURL, nil)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error creating request: %v", err), http.StatusInternalServerError)
			return
		}

		// Forward headers from the original request
		req.Header = r.Header.Clone()

		resp, err := insecureClient.Do(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error performing request: %v", err), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		// Forward the response to the client
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			log.Errorf("Error writing response: %v", err)
		}

		log.Infof("Get %q, forwarded to %q with status %d", r.URL, targetURL, resp.StatusCode)
	}
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
