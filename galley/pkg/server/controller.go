// Copyright 2018 Istio Authors
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

package server

import (
	"time"

	"istio.io/istio/pkg/probe"
)

const (
	//DefaultProbeCheckInterval defines default probeCheck interval
	DefaultProbeCheckInterval = 2 * time.Second
	//DefaultLivenessProbeFilePath defines default livenessProbe filePath
	DefaultLivenessProbeFilePath = "/healthLiveness"
	//DefaultReadinessProbeFilePath defines default readinessProbe filePath
	DefaultReadinessProbeFilePath = "/healthReadiness"
)

//StartProbeCheck start probe check for Galley
func StartProbeCheck(livenessProbeController, readinessProbeController probe.Controller, stop <-chan struct{}) {
	if livenessProbeController != nil {
		livenessProbeController.Start()
		defer livenessProbeController.Close()
	}
	if readinessProbeController != nil {
		readinessProbeController.Start()
		defer readinessProbeController.Close()
	}
	<-stop

}
