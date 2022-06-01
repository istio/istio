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
	"archive/tar"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"

	"istio.io/pkg/log"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/cache"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

var (
	port      = flag.Int("port", 8080, "port to run proxy on")
	cachePath = flag.String("cachepath", "", "directory for image cache. If it is an empty string, create/use a temp dir.")
)

var re = regexp.MustCompile(`^/(?P<Reference>.+)/tag/(?P<Tag>[^/]+)/(?P<FilePath>.+)$`)

type Handler struct {
	cache cache.Cache
}

type readerCloser struct {
	io.Reader
	io.Closer
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ready" {
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Debugf("requested URL: %v", r.URL)
	matches := re.FindStringSubmatch(r.URL.Path)
	if matches == nil {
		log.Errorf("%q is not matched with regex scheme %q", r.URL.Path, re)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ref := matches[re.SubexpIndex("Reference")] + ":" + matches[re.SubexpIndex("Tag")]
	filePath := matches[re.SubexpIndex("FilePath")]

	log.Debugf("Retrieve file from oci://%v:%v", ref, filePath)

	reader, err := h.extractFile(ref, filePath)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		log.Errorf("failed to extract the file %q from %q: %v", filePath, ref, err)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	if _, err = io.Copy(w, reader); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf("failed to copy the contents of the file to the response writer: %v", err)
		return
	}
}

func (h *Handler) extractFile(ref string, filePath string) (io.ReadCloser, error) {
	t := remote.DefaultTransport.Clone()
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	img, err := crane.Pull(ref, crane.WithTransport(t))
	if err != nil {
		return nil, fmt.Errorf("failed to pull the image %q: %v", ref, err)
	}

	img = cache.Image(img, h.cache)

	ir := mutate.Extract(img)
	tr := tar.NewReader(ir)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if hdr.Name == filePath {
			return readerCloser{
				Reader: tr,
				Closer: ir,
			}, nil
		}
	}
	return nil, fmt.Errorf("%s not found in the archive", filePath)
}

func main() {
	flag.Parse()
	cpath := *cachePath
	var err error
	if cpath == "" {
		cpath, err = ioutil.TempDir("", "")
		if err != nil {
			log.Fatal(err)
			return
		}
	}

	s := &http.Server{
		Addr: fmt.Sprintf(":%d", *port),
		Handler: &Handler{
			cache.NewFilesystemCache(cpath),
		},
	}

	log.Infof("httpimageproxy is starting at %d", *port)
	if err := s.ListenAndServe(); err != nil {
		log.Error(err)
	}
}
