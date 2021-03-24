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

package gcpmonitoring

import (
	"google.golang.org/genproto/googleapis/api/monitoredres"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

var (
	useStackdriverLoggingFormat = env.RegisterBoolVar(
		"USE_STACKDRIVER_LOGGING_FORMAT",
		true,
		"Whether or not to use the Stackdriver-compatible JSON logging format for application logs")

	teeLogsToStackdriver = env.RegisterBoolVar(
		"TEE_LOGS_TO_STACKDRIVER",
		false,
		"Whether or not to send application logs directly to Stackdriver in addition to stdout/stderr")
)

func ASMLogOptions() *log.Options {
	opts := log.DefaultOptions()
	if useStackdriverLoggingFormat.Get() {
		opts = opts.WithStackdriverLoggingFormat()
	}
	if teeLogsToStackdriver.Get() {
		meta := platform.NewGCP().Metadata()
		proj := meta[platform.GCPProject]
		loc := meta[platform.GCPLocation]
		mesh := meshUID
		if mesh == "" {
			mesh = meshUIDFromPlatformMeta(meta)
		}
		opts = opts.WithTeeToStackdriver(proj, "istiod", loggingMonitoredResource(proj, loc, mesh))
	}
	return opts
}

func loggingMonitoredResource(proj, loc, meshUID string) *monitoredres.MonitoredResource {
	owner := "asm"
	if isCloudRun() {
		owner = "asm-managed"
	}
	return &monitoredres.MonitoredResource{
		Type: "istio_control_plane",
		Labels: map[string]string{
			"project_id": proj,
			"mesh_uid":   meshUID,
			"location":   loc,
			"revision":   revisionLabel(),
			"build_id":   version.Info.Version,
			"owner":      owner,
		},
	}
}

func meshUIDFromPlatformMeta(meta map[string]string) string {
	uid := "unknown"
	if pid, ok := meta[platform.GCPProjectNumber]; ok && pid != "" {
		uid = "proj-" + pid
	}
	return uid
}
