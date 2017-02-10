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

package envoy

import (
	"net/http"
	"strconv"

	restful "github.com/emicklei/go-restful"
	"github.com/golang/glog"

	"istio.io/manager/model"
)

// DiscoveryService publishes services, clusters, and routes for proxies
type DiscoveryService struct {
	services model.ServiceDiscovery
	server   *http.Server
}

type hosts struct {
	Hosts []host `json:"hosts"`
}

type host struct {
	Address string `json:"ip_address"`
	Port    int    `json:"port"`

	// Weight is an integer in the range [1, 100] or empty
	Weight int `json:"load_balancing_weight,omitempty"`
}

type clusters struct {
	Clusters []Cluster `json:"clusters"`
}

// NewDiscoveryService creates an Envoy discovery service on a given port
func NewDiscoveryService(services model.ServiceDiscovery, port int) *DiscoveryService {
	out := &DiscoveryService{
		services: services,
	}
	container := restful.NewContainer()
	out.Register(container)
	out.server = &http.Server{Addr: ":" + strconv.Itoa(port), Handler: container}
	return out
}

// Register adds routes a web service container
func (ds *DiscoveryService) Register(container *restful.Container) {
	ws := &restful.WebService{}
	ws.Produces(restful.MIME_JSON)
	ws.Route(ws.
		GET("/v1/registration/{service-key}").
		To(ds.ListEndpoints).
		Doc("SDS registration").
		Param(ws.PathParameter("service-key", "tuple of service name and tag name").DataType("string")).
		Writes(hosts{}))
	ws.Route(ws.
		GET("/v1/clusters/{service-cluster}/{service-node}").
		To(ds.ListClusters).
		Doc("CDS registration").
		Param(ws.PathParameter("service-cluster", "").DataType("string")).
		Param(ws.PathParameter("service-node", "").DataType("string")).
		Writes(clusters{}))
	container.Add(ws)
}

// Run starts the server and blocks
func (ds *DiscoveryService) Run() {
	glog.Infof("Starting discovery service at %v", ds.server.Addr)
	if err := ds.server.ListenAndServe(); err != nil {
		glog.Warning(err)
	}
}

// ListEndpoints responds to SDS requests
func (ds *DiscoveryService) ListEndpoints(request *restful.Request, response *restful.Response) {
	key := request.PathParameter("service-key")
	hostname, ports, tags := model.ParseServiceKey(key)
	out := make([]host, 0)
	for _, ep := range ds.services.Instances(hostname, ports.GetNames(), tags) {
		out = append(out, host{
			Address: ep.Endpoint.Address,
			Port:    ep.Endpoint.Port,
		})
	}
	if err := response.WriteEntity(hosts{out}); err != nil {
		glog.Warning(err)
	}
}

// ListClusters responds to CDS requests
func (ds *DiscoveryService) ListClusters(request *restful.Request, response *restful.Response) {
	_ = ds.services.Services()
	// TODO: fix this
	/*
		if err := response.WriteEntity(clusters{buildClusters(svc)}); err != nil {
			glog.Warning(err)
		}
	*/
}
