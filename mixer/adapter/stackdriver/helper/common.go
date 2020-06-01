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

package helper

import (
	"os"

	gapiopts "google.golang.org/api/option"

	"istio.io/istio/mixer/adapter/stackdriver/config"
	"istio.io/istio/mixer/pkg/adapter"
)

type shouldFillFn func() bool
type metadataFn func() (string, error)

// Metadata keeps metadata about the project which this stackdriver adapter is running on.
type Metadata struct {
	ProjectID   string
	Location    string
	ClusterName string
}

// MetadataGenerator creates metadata based on the given metadata functions.
type MetadataGenerator interface {
	GenerateMetadata() Metadata
}

type metadataGeneratorImpl struct {
	shouldFill    shouldFillFn
	projectIDFn   metadataFn
	locationFn    metadataFn
	clusterNameFn metadataFn
}

// NewMetadataGenerator creates a MetadataGenerator with the given functions.
func NewMetadataGenerator(shouldFill shouldFillFn, projectIDFn, locationFn, clusterNameFn metadataFn) MetadataGenerator {
	return &metadataGeneratorImpl{
		shouldFill:    shouldFill,
		projectIDFn:   projectIDFn,
		locationFn:    locationFn,
		clusterNameFn: clusterNameFn,
	}
}

// GenerateMetadata generates a Metadata struct if the condition is fulfilled.
func (mg *metadataGeneratorImpl) GenerateMetadata() Metadata {
	var md Metadata
	if !mg.shouldFill() {
		return md
	}
	if pid, err := mg.projectIDFn(); err == nil {
		md.ProjectID = pid
	}
	if l, err := mg.locationFn(); err == nil {
		md.Location = l
	}
	if cn, err := mg.clusterNameFn(); err == nil {
		md.ClusterName = cn
	}
	return md
}

// FillProjectMetadata fills project metadata for the given map if the key matches and the value is empty.
func (md *Metadata) FillProjectMetadata(in map[string]string) {
	for key, val := range in {
		if val != "" {
			continue
		}
		if key == "project_id" {
			in[key] = md.ProjectID
		}
		if key == "location" || key == "zone" {
			in[key] = md.Location
		}
		if key == "cluster_name" {
			in[key] = md.ClusterName
		}
	}
}

// ToOpts converts the Stackdriver config params to options for configuring Stackdriver clients.
func ToOpts(cfg *config.Params, logger adapter.Logger) (opts []gapiopts.ClientOption) {
	switch cfg.Creds.(type) {
	case *config.Params_ApiKey:
		logger.Warningf("API Key is no longer supported by gRPC client. Use ServiceAccountPath instead.")
	case *config.Params_ServiceAccountPath:
		path := cfg.GetServiceAccountPath()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			opts = append(opts, gapiopts.WithCredentialsFile(path))
		} else {
			logger.Warningf("could not find %v, using Application Default Credentials instead", path)
		}
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
