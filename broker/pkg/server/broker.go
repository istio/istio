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

// Package server provides HTTP open service broker API server bindings.
package server

import (
	"fmt"
	"net/http"

	"github.com/golang/glog"
	"github.com/gorilla/mux"

	"istio.io/istio/broker/pkg/controller"
	"istio.io/istio/broker/pkg/model/config"
	"istio.io/istio/broker/pkg/platform/kube/crd"
)

// Server data
type Server struct {
	ctr *controller.Controller
}

// CreateServer creates a broker server.
func CreateServer(kubeconfig string) (*Server, error) {
	cc, err := crd.NewClient(kubeconfig, config.BrokerConfigTypes)
	if err != nil {
		return nil, err
	}
	c, err := controller.CreateController(config.MakeBrokerConfigStore(cc))
	if err != nil {
		return nil, err
	}

	return &Server{
		c,
	}, nil
}

// Start runs the server and listen on port.
func (s *Server) Start(port uint16) {
	router := mux.NewRouter()

	router.HandleFunc("/v2/catalog", s.ctr.Catalog).Methods("GET")

	http.Handle("/", router)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		glog.Errorf("Unable to start server: %v", err)
	}
}
