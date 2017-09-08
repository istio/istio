// Copyright 2017 the Istio Authors.
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

package helper

import (
	"fmt"

	gapiopts "google.golang.org/api/option"

	"istio.io/mixer/adapter/stackdriver/config"
)

// ToOpts converts the Stackdriver config params to options for configuring Stackdriver clients.
func ToOpts(cfg *config.Params) (opts []gapiopts.ClientOption) {
	switch cfg.Creds.(type) {
	case *config.Params_ApiKey:
		opts = append(opts, gapiopts.WithAPIKey(cfg.GetApiKey()))
	case *config.Params_ServiceAccountPath:
		opts = append(opts, gapiopts.WithServiceAccountFile(cfg.GetServiceAccountPath()))
	case *config.Params_AppCredentials:
		// When using default app credentials the SDK handles everything for us.
	}
	if cfg.Endpoint != "" {
		opts = append(opts, gapiopts.WithEndpoint(cfg.Endpoint))
	}
	return
}

// ToStringMap converts a map[string]interface{} to a map[string]string using fmt.Sprintf(%v)
func ToStringMap(in map[string]interface{}) map[string]string {
	out := make(map[string]string, len(in))
	for key, val := range in {
		out[key] = fmt.Sprintf("%v", val)
	}
	return out
}
