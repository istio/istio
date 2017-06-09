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

package apiserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"istio.io/pilot/cmd/version"
	"istio.io/pilot/model"

	restful "github.com/emicklei/go-restful"
	"github.com/golang/glog"
)

const (
	kind      = "kind"
	name      = "name"
	namespace = "namespace"
)

// APIServiceOptions are the options available for configuration on the API
// Version is the API version e.g. v1 for /v1/config
type APIServiceOptions struct {
	Version  string
	Port     int
	Registry model.ConfigStore
}

// API is the server wrapper that listens for incoming requests to the config and processes them
type API struct {
	server   *http.Server
	version  string
	registry model.ConfigStore
}

// NewAPI creates a new instance of the API using the options passed to it
// It returns a pointer to the newly created API
func NewAPI(o APIServiceOptions) *API {
	out := &API{
		version:  o.Version,
		registry: o.Registry,
	}
	container := restful.NewContainer()
	out.Register(container)
	out.server = &http.Server{Addr: ":" + strconv.Itoa(o.Port), Handler: container}
	return out
}

// Register adds the routes to the restful container
func (api *API) Register(container *restful.Container) {
	ws := &restful.WebService{}
	ws.Consumes(restful.MIME_JSON)
	ws.Produces(restful.MIME_JSON)
	ws.Path(fmt.Sprintf("/%s", api.version))

	ws.Route(ws.
		GET(fmt.Sprintf("/config/{%s}/{%s}/{%s}", kind, namespace, name)).
		To(api.GetConfig).
		Doc("Get a config").
		Writes(Config{}))

	ws.Route(ws.
		POST(fmt.Sprintf("/config/{%s}/{%s}/{%s}", kind, namespace, name)).
		To(api.AddConfig).
		Doc("Add a config").
		Reads(Config{}))

	ws.Route(ws.
		PUT(fmt.Sprintf("/config/{%s}/{%s}/{%s}", kind, namespace, name)).
		To(api.UpdateConfig).
		Doc("Update a config").
		Reads(Config{}))

	ws.Route(ws.
		DELETE(fmt.Sprintf("/config/{%s}/{%s}/{%s}", kind, namespace, name)).
		To(api.DeleteConfig).
		Doc("Delete a config"))

	ws.Route(ws.
		GET(fmt.Sprintf("/config/{%s}/{%s}", kind, namespace)).
		To(api.ListConfigs).
		Doc("List all configs for kind in a given namespace").
		Writes([]Config{}))

	ws.Route(ws.
		GET(fmt.Sprintf("/config/{%s}", kind)).
		To(api.ListConfigs).
		Doc("List all configs for kind in across all namespaces").
		Writes([]Config{}))

	ws.Route(ws.
		GET("/health").
		To(api.Status).
		Doc("Returns 200 when the container is up"))

	// Return information about the version of API server running
	ws.Route(ws.
		GET("/version").
		To(api.Version).
		Doc("Returns version information").
		Writes(version.BuildInfo{}))

	container.Add(ws)
}

// Run calls listen and serve on the API server
func (api *API) Run() {
	glog.Infof("Starting api at %v", api.server.Addr)
	if err := api.server.ListenAndServe(); err != nil {
		glog.Warning(err)
	}
}

// Shutdown calls `Shutdown(nil)` on the API server
func (api *API) Shutdown(ctx context.Context) {
	if api != nil && api.server != nil {
		if err := api.server.Shutdown(ctx); err != nil {
			glog.Warning(err)
		}
	}
}
