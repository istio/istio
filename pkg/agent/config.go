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

package agent

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"os"

	multierror "github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pkg/log"
)

// Config generation main functions.
// The general flow of the generation process consists of the following steps:
// - routes are created for each destination, with referenced clusters stored as a special field
// - routes are organized into listeners for inbound and outbound traffic
// - clusters are aggregated and normalized across routes
// - extra policies and filters are added by additional passes over abstract config structures
// - configuration elements are de-duplicated and ordered in a canonical way

// WriteFile saves config to a file
func (conf *Config) WriteFile(fname string) error {
	if log.InfoEnabled() {
		log.Infof("writing configuration to %s", fname)
		if err := conf.Write(os.Stderr); err != nil {
			log.Errora(err)
		}
	}

	file, err := os.Create(fname)
	if err != nil {
		return err
	}

	if err := conf.Write(file); err != nil {
		err = multierror.Append(err, file.Close())
		return err
	}

	return file.Close()
}

func (conf *Config) Write(w io.Writer) error {
	out, err := json.MarshalIndent(&conf, "", "  ")
	if err != nil {
		return err
	}

	_, err = w.Write(out)
	return err
}

// buildConfig creates a proxy config with discovery services and admin port
// it creates config for Ingress, Egress and Sidecar proxies
// TODO(costin): move to agent package
func buildConfig(config meshconfig.ProxyConfig, pilotSAN []string) *Config {
	listeners := Listeners{}

	clusterRDS := buildCluster(config.DiscoveryAddress, RDSName, config.ConnectTimeout)
	clusterLDS := buildCluster(config.DiscoveryAddress, LDSName, config.ConnectTimeout)
	clusters := Clusters{clusterRDS, clusterLDS}

	out := &Config{
		Listeners: listeners,
		LDS: &LDSCluster{
			Cluster:        LDSName,
			RefreshDelayMs: protoDurationToMS(config.DiscoveryRefreshDelay),
		},
		Admin: Admin{
			AccessLogPath: DefaultAccessLog,
			Address:       fmt.Sprintf("tcp://%s:%d", LocalhostAddress, config.ProxyAdminPort),
		},
		ClusterManager: ClusterManager{
			Clusters: clusters,
			SDS: &DiscoveryCluster{
				Cluster:        buildCluster(config.DiscoveryAddress, SDSName, config.ConnectTimeout),
				RefreshDelayMs: protoDurationToMS(config.DiscoveryRefreshDelay),
			},
			CDS: &DiscoveryCluster{
				Cluster:        buildCluster(config.DiscoveryAddress, CDSName, config.ConnectTimeout),
				RefreshDelayMs: protoDurationToMS(config.DiscoveryRefreshDelay),
			},
		},
		StatsdUDPIPAddress: config.StatsdUdpAddress,
	}

	// apply auth policies
	switch config.ControlPlaneAuthPolicy {
	case meshconfig.AuthenticationPolicy_NONE:
		// do nothing
	case meshconfig.AuthenticationPolicy_MUTUAL_TLS:
		sslContext := buildClusterSSLContext(model.AuthCertsPath, pilotSAN)
		clusterRDS.SSLContext = sslContext
		clusterLDS.SSLContext = sslContext
		out.ClusterManager.SDS.Cluster.SSLContext = sslContext
		out.ClusterManager.CDS.Cluster.SSLContext = sslContext
	default:
		panic(fmt.Sprintf("ControlPlaneAuthPolicy cannot be %v\n", config.ControlPlaneAuthPolicy))
	}

	if config.ZipkinAddress != "" {
		out.ClusterManager.Clusters = append(out.ClusterManager.Clusters,
			buildCluster(config.ZipkinAddress, ZipkinCollectorCluster, config.ConnectTimeout))
		out.Tracing = buildZipkinTracing()
	}

	return out
}

func truncateClusterName(name string) string {
	if len(name) > MaxClusterNameLength {
		prefix := name[:MaxClusterNameLength-sha1.Size*2]
		sum := sha1.Sum([]byte(name))
		return fmt.Sprintf("%s%x", prefix, sum)
	}
	return name
}
