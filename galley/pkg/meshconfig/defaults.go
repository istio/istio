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

package meshconfig

import (
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/api/mesh/v1alpha1"
)

// Default mesh configuration
func Default() v1alpha1.MeshConfig {
	return v1alpha1.MeshConfig{
		MixerCheckServer:      "",
		MixerReportServer:     "",
		DisablePolicyChecks:   false,
		PolicyCheckFailOpen:   false,
		ProxyListenPort:       15001,
		ConnectTimeout:        types.DurationProto(1 * time.Second),
		IngressClass:          "istio",
		IngressControllerMode: v1alpha1.MeshConfig_STRICT,
		EnableTracing:         true,
		AccessLogFile:         "/dev/stdout",
		SdsUdsPath:            "",
		OutboundTrafficPolicy: &v1alpha1.MeshConfig_OutboundTrafficPolicy{Mode: v1alpha1.MeshConfig_OutboundTrafficPolicy_ALLOW_ANY},
	}
}
