// Copyright 2017 Istio Authors
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

package inject

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

	restful "github.com/emicklei/go-restful"
	"github.com/golang/glog"
)

const (
	contentTypeYAML = "application/yaml"
)

// HTTPServer implements an HTTP endpoint equivalent of the k8s
// sidecar initializer.
type HTTPServer struct {
	config    *Config
	server    *http.Server
	container *restful.Container
}

// NewHTTPServer creates a new HTTP server.
func NewHTTPServer(port int, config *Config) *HTTPServer {
	container := restful.NewContainer()
	r := &HTTPServer{
		server: &http.Server{
			Addr:    fmt.Sprintf(":%v", strconv.Itoa(port)),
			Handler: container,
		},
		config:    config,
		container: container,
	}

	ws := &restful.WebService{}
	ws.Consumes(contentTypeYAML)
	ws.Produces(contentTypeYAML)

	ws.Route(ws.
		POST("/inject").
		To(r.inject).
		Doc("Inject sidecar into Kubernetes resource"))

	container.Add(ws)

	return r
}

func onError(err error, status int, body []byte, response *restful.Response) {
	glog.WarningDepth(1, err.Error())
	response.WriteHeader(status)
	if _, err := response.Write(body); err != nil {
		glog.WarningDepth(1, err.Error())
	}
}

func (r *HTTPServer) inject(request *restful.Request, response *restful.Response) {
	// TODO - assume Content-Type=YAML for parity with
	// kube-inject. This could be extended to support JSON.
	response.Header().Set("Content-Type", contentTypeYAML)

	// Make a copy so we can write the original copy back in case of
	// internal errors.
	body, err := ioutil.ReadAll(request.Request.Body)
	if err != nil {
		onError(err, http.StatusBadRequest, body, response)
		return
	}

	var out bytes.Buffer
	if err := IntoResourceFile(r.config, bytes.NewBuffer(body), &out); err != nil {
		onError(err, http.StatusInternalServerError, body, response)
		return
	}

	if _, err := response.Write(out.Bytes()); err != nil {
		glog.Warning(err.Error())
	}
}

// Run runs the HTTP server.
func (r *HTTPServer) Run(stopCh <-chan struct{}) {
	glog.Infof("Starting HTTP service at %v", r.server.Addr)
	go func() {
		<-stopCh
		r.server.Close() // nolint: errcheck
	}()
	if err := r.server.ListenAndServe(); err != nil {
		glog.Error(err.Error())
	}
}
