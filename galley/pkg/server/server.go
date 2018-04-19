//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"time"

	"istio.io/istio/galley/pkg/kube"
	"istio.io/istio/galley/pkg/kube/client"
	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/pkg/log"
)

type Server struct {
	rt *runtime.Processor
}

func New(k kube.Kube, resyncPeriod time.Duration) (*Server, error) {
	src, err := client.NewSource(k, resyncPeriod)
	if err != nil {
		return nil, nil
	}

	dist := client.NewDistributor(k)
	rt := runtime.New(src, dist)

	s := &Server{
		rt: rt,
	}

	return s, nil
}

func (s *Server) Start() error {
	log.Info("Starting server...")
	return s.rt.Start()
}

func (s *Server) Stop() {
	s.rt.Stop()
}
