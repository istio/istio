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

package envoy

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// ClusterTypeStrictDNS name for clusters of type 'strict_dns'
	ClusterTypeStrictDNS = "strict_dns"

	// LDSName is the name of listener-discovery-service (LDS) cluster
	LDSName = "lds"

	// RDSName is the name of route-discovery-service (RDS) cluster
	RDSName = "rds"

	// SDSName is the name of service-discovery-service (SDS) cluster
	SDSName = "sds"

	// CDSName is the name of cluster-discovery-service (CDS) cluster
	CDSName = "cds"

	// DefaultAccessLog is the name of the log channel (stdout in docker environment)
	DefaultAccessLog = "/dev/stdout"

	// LocalhostAddress for local binding
	LocalhostAddress = "127.0.0.1"

	// DefaultLbType defines the default load balancer policy
	DefaultLbType = "round_robin"

	// ZipkinTraceDriverType denotes the Zipkin HTTP trace driver
	ZipkinTraceDriverType = "zipkin"

	// ZipkinCollectorCluster denotes the cluster where zipkin server is running
	ZipkinCollectorCluster = "zipkin"

	// ZipkinCollectorEndpoint denotes the REST endpoint where Envoy posts Zipkin spans
	ZipkinCollectorEndpoint = "/api/v1/spans"

	// MaxClusterNameLength is the maximum cluster name length
	MaxClusterNameLength = 189 // TODO: use MeshConfig.StatNameLength instead
)

// convertDuration converts to golang duration and logs errors
func convertDuration(d *duration.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, err := ptypes.Duration(d)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
	}
	return dur
}

func protoDurationToMS(dur *duration.Duration) int64 {
	return int64(convertDuration(dur) / time.Millisecond)
}

// Config defines the schema for Envoy JSON configuration format
type Config struct {
	RootRuntime        *RootRuntime   `json:"runtime,omitempty"`
	Listeners          Listeners      `json:"listeners"`
	LDS                *LDSCluster    `json:"lds,omitempty"`
	Admin              Admin          `json:"admin"`
	ClusterManager     ClusterManager `json:"cluster_manager"`
	StatsdUDPIPAddress string         `json:"statsd_udp_ip_address,omitempty"`
	Tracing            *Tracing       `json:"tracing,omitempty"`

	// Special value used to hash all referenced values (e.g. TLS secrets)
	Hash []byte `json:"-"`
}

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

// BuildConfig creates a proxy config with discovery services and admin port
// it creates config for Ingress, Egress and Sidecar proxies
// TODO: remove after new agent package is done
func BuildConfig(config meshconfig.ProxyConfig, pilotSAN []string) *Config {
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

func buildCluster(address, name string, timeout *duration.Duration) *Cluster {
	return &Cluster{
		Name:             name,
		Type:             ClusterTypeStrictDNS,
		ConnectTimeoutMs: protoDurationToMS(timeout),
		LbType:           DefaultLbType,
		Hosts: []Host{
			{
				URL: "tcp://" + address,
			},
		},
	}
}

// buildClusterSSLContext returns an SSLContextWithSAN struct with VerifySubjectAltName.
// The list of service accounts may be empty but not nil.
func buildClusterSSLContext(certsDir string, serviceAccounts []string) *SSLContextWithSAN {
	return &SSLContextWithSAN{
		CertChainFile:        path.Join(certsDir, model.CertChainFilename),
		PrivateKeyFile:       path.Join(certsDir, model.KeyFilename),
		CaCertFile:           path.Join(certsDir, model.RootCertFilename),
		VerifySubjectAltName: serviceAccounts,
	}
}

func buildZipkinTracing() *Tracing {
	return &Tracing{
		HTTPTracer: HTTPTracer{
			HTTPTraceDriver: HTTPTraceDriver{
				HTTPTraceDriverType: ZipkinTraceDriverType,
				HTTPTraceDriverConfig: HTTPTraceDriverConfig{
					CollectorCluster:  ZipkinCollectorCluster,
					CollectorEndpoint: ZipkinCollectorEndpoint,
				},
			},
		},
	}
}
