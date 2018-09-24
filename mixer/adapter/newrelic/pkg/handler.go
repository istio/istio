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
package pkg

import (
	"istio.io/istio/mixer/template/metric"
)

// JobQueue will hold all incoming data as a queue
var JobQueue chan JobRequest

// HandleInstances extracts telemetry data from incoming request from mixer and put
// it into the global queue
func HandleInstances(insts []*metric.InstanceMsg) {
	job := JobRequest{PayLoad: insts}
	JobQueue <- job
}

func init() {
	JobQueue = make(chan JobRequest, 1024)
}
