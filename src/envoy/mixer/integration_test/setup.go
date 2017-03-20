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

package test

import (
	"log"
)

const (
	// These ports should match with used envoy.conf
	// Default is using one in this folder.
	ServerProxyPort = 29090
	ClientProxyPort = 27070
	MixerPort       = 29091
	BackendPort     = 28080
)

type TestSetup struct {
	envoy   *Envoy
	mixer   *MixerServer
	backend *HttpServer
}

func SetUp() (ts TestSetup, err error) {
	ts.envoy, err = NewEnvoy()
	if err != nil {
		log.Printf("unable to create Envoy %v", err)
	} else {
		ts.envoy.Start()
	}

	ts.mixer, err = NewMixerServer(MixerPort)
	if err != nil {
		log.Printf("unable to create mixer server %v", err)
	} else {
		ts.mixer.Start()
	}

	ts.backend, err = NewHttpServer(BackendPort)
	if err != nil {
		log.Printf("unable to create HTTP server %v", err)
	} else {
		ts.backend.Start()
	}
	return ts, err
}

func (ts *TestSetup) TearDown() {
	ts.envoy.Stop()
	ts.mixer.Stop()
	ts.backend.Stop()
}
