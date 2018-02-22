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
	gapiopts "google.golang.org/api/option"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/pkg/adapter"
)

type shouldFillFn func(*config.Params) bool
type projectIDFn func() (string, error)

// ProjectIDFiller checks and fills project id for stack adapter config.
type ProjectIDFiller struct {
	shouldFill shouldFillFn
	projectID  projectIDFn
}

// NewProjectIDFiller creates a project id filler for stackdriver adapter config with the given functions.
func NewProjectIDFiller(s shouldFillFn, p projectIDFn) *ProjectIDFiller {
	return &ProjectIDFiller{s, p}
}

// FillProjectID tries to fill project ID in adapter config if it is empty.
func (p *ProjectIDFiller) FillProjectID(c adapter.Config) {
	cfg := c.(*config.Params)
	if !p.shouldFill(cfg) {
		return
	}
	if pid, err := p.projectID(); err == nil {
		cfg.ProjectId = pid
	}
}

// ToOpts converts the Stackdriver config params to options for configuring Stackdriver clients.
func ToOpts(cfg *config.Params) (opts []gapiopts.ClientOption) {
	switch cfg.Creds.(type) {
	case *config.Params_ApiKey:
		opts = append(opts, gapiopts.WithAPIKey(cfg.GetApiKey()))
	case *config.Params_ServiceAccountPath:
		opts = append(opts, gapiopts.WithCredentialsFile(cfg.GetServiceAccountPath()))
	case *config.Params_AppCredentials:
		// When using default app credentials the SDK handles everything for us.
	}
	if cfg.Endpoint != "" {
		opts = append(opts, gapiopts.WithEndpoint(cfg.Endpoint))
	}
	return
}

// ToStringMap converts a map[string]interface{} to a map[string]string
func ToStringMap(in map[string]interface{}) map[string]string {
	out := make(map[string]string, len(in))
	for key, val := range in {
		out[key] = adapter.Stringify(val)
	}
	return out
}
