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

package istioagent

import (
	"encoding/json"
	"fmt"
	"path"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"istio.io/istio/pkg/file"

	"google.golang.org/protobuf/types/known/structpb"
)

// TODO use structs from gRPC lib if created/exported
type grpcBootstrap struct {
	XDSServer     xdsServer                      `json:"xds_server,omitempty"`
	Node          *corev3.Node                   `json:"node,omitempty"`
	CertProviders map[string]certificateProvider `json:"certificate_providers,omitempty"`
}

type channelCreds struct {
	Type   string      `json:"type,omitempty"`
	Config interface{} `json:"config,omitempty"`
}

type xdsServer struct {
	ServerURI      string         `json:"server_uri,omitempty"`
	ChannelCreds   []channelCreds `json:"channel_creds,omitempty"`
	ServerFeatures []string       `json:"server_features,omitempty"`
}

type certificateProvider struct {
	Name   string      `json:"name,omitempty"`
	Config interface{} `json:"config,omitempty"`
}

type fileWatcherCertProviderConfig struct {
	CertificateFile   string               `json:"certificate_file,omitempty"`
	PrivateKeyFile    string               `json:"private_key_file,omitempty"`
	CACertificateFile string               `json:"ca_certificate_file,omitempty"`
	RefreshDuration   *durationpb.Duration `json:"refresh_interval,omitempty"`
}

func (a *Agent) generateGRPCBootstrap() error {
	// generate metadata
	node, err := a.generateNodeMetadata()
	if err != nil {
		return fmt.Errorf("failed generating node metadata: %v", err)
	}
	xdsMeta, err := structpb.NewStruct(node.RawMetadata)
	if err != nil {
		return fmt.Errorf("failed converting to xds metadata: %v", err)
	}

	// TODO secure control plane channel (most likely JWT + TLS, but possibly allow mTLS)
	serverURI := a.proxyConfig.DiscoveryAddress
	if a.cfg.ProxyXDSViaAgent {
		serverURI = "localhost:15010"
	}

	bootstrap := grpcBootstrap{
		XDSServer: xdsServer{ServerURI: serverURI},
		Node: &corev3.Node{
			Id:       node.ID,
			Locality: node.Locality,
			Metadata: xdsMeta,
		},
	}

	if a.secOpts.OutputKeyCertToDir != "" {
		bootstrap.CertProviders = map[string]certificateProvider{
			"default": {
				Name: "file_watcher",
				Config: fileWatcherCertProviderConfig{
					PrivateKeyFile:    path.Join(a.secOpts.OutputKeyCertToDir, "key.pem"),
					CertificateFile:   path.Join(a.secOpts.OutputKeyCertToDir, "cert-chain.pem"),
					CACertificateFile: path.Join(a.secOpts.OutputKeyCertToDir, "root-cert.pem"),
					// TODO use a more appropriate interval
					RefreshDuration: durationpb.New(15 * time.Minute),
				},
			},
		}
	}
	jsonData, err := json.MarshalIndent(bootstrap, "", "  ")
	if err != nil {
		return err
	}
	if err := file.AtomicWrite(a.cfg.GRPCBootstrapPath, jsonData, 0o644); err != nil {
		return fmt.Errorf("failed writing to %s: %v", a.cfg.GRPCBootstrapPath, err)
	}

	return nil
}
