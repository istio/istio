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

package grpcxds

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/log"
)

const (
	ServerListenerNamePrefix = "xds.istio.io/grpc/lds/inbound/"
	// ServerListenerNameTemplate for the name of the Listener resource to subscribe to for a gRPC
	// server. If the token `%s` is present in the string, all instances of the
	// token will be replaced with the server's listening "IP:port" (e.g.,
	// "0.0.0.0:8080", "[::]:8080").
	ServerListenerNameTemplate = ServerListenerNamePrefix + "%s"
)

// Bootstrap contains the general structure of what's expected by GRPC's XDS implementation.
// See https://github.com/grpc/grpc-go/blob/master/xds/internal/xdsclient/bootstrap/bootstrap.go
// TODO use structs from gRPC lib if created/exported
type Bootstrap struct {
	XDSServers                 []XdsServer                    `json:"xds_servers,omitempty"`
	Node                       *corev3.Node                   `json:"node,omitempty"`
	CertProviders              map[string]CertificateProvider `json:"certificate_providers,omitempty"`
	ServerListenerNameTemplate string                         `json:"server_listener_resource_name_template,omitempty"`
}

type ChannelCreds struct {
	Type   string `json:"type,omitempty"`
	Config any    `json:"config,omitempty"`
}

type XdsServer struct {
	ServerURI      string         `json:"server_uri,omitempty"`
	ChannelCreds   []ChannelCreds `json:"channel_creds,omitempty"`
	ServerFeatures []string       `json:"server_features,omitempty"`
}

type CertificateProvider struct {
	PluginName string `json:"plugin_name,omitempty"`
	Config     any    `json:"config,omitempty"`
}

func (cp *CertificateProvider) UnmarshalJSON(data []byte) error {
	var dat map[string]*json.RawMessage
	if err := json.Unmarshal(data, &dat); err != nil {
		return err
	}
	*cp = CertificateProvider{}

	if pluginNameVal, ok := dat["plugin_name"]; ok {
		if err := json.Unmarshal(*pluginNameVal, &cp.PluginName); err != nil {
			log.Warnf("failed parsing plugin_name in certificate_provider: %v", err)
		}
	} else {
		log.Warnf("did not find plugin_name in certificate_provider")
	}

	if configVal, ok := dat["config"]; ok {
		var err error
		switch cp.PluginName {
		case FileWatcherCertProviderName:
			config := FileWatcherCertProviderConfig{}
			err = json.Unmarshal(*configVal, &config)
			cp.Config = config
		default:
			config := FileWatcherCertProviderConfig{}
			err = json.Unmarshal(*configVal, &config)
			cp.Config = config
		}
		if err != nil {
			log.Warnf("failed parsing config in certificate_provider: %v", err)
		}
	} else {
		log.Warnf("did not find config in certificate_provider")
	}

	return nil
}

const FileWatcherCertProviderName = "file_watcher"

type FileWatcherCertProviderConfig struct {
	CertificateFile   string          `json:"certificate_file,omitempty"`
	PrivateKeyFile    string          `json:"private_key_file,omitempty"`
	CACertificateFile string          `json:"ca_certificate_file,omitempty"`
	RefreshDuration   json.RawMessage `json:"refresh_interval,omitempty"`
}

func (c *FileWatcherCertProviderConfig) FilePaths() []string {
	return []string{c.CertificateFile, c.PrivateKeyFile, c.CACertificateFile}
}

// FileWatcherProvider returns the FileWatcherCertProviderConfig if one exists in CertProviders
func (b *Bootstrap) FileWatcherProvider() *FileWatcherCertProviderConfig {
	if b == nil || b.CertProviders == nil {
		return nil
	}
	for _, provider := range b.CertProviders {
		if provider.PluginName == FileWatcherCertProviderName {
			cfg, ok := provider.Config.(FileWatcherCertProviderConfig)
			if !ok {
				return nil
			}
			return &cfg
		}
	}
	return nil
}

// LoadBootstrap loads a Bootstrap from the given file path.
func LoadBootstrap(file string) (*Bootstrap, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	b := &Bootstrap{}
	if err := json.Unmarshal(data, b); err != nil {
		return nil, err
	}
	return b, err
}

type GenerateBootstrapOptions struct {
	Node             *model.Node
	XdsUdsPath       string
	DiscoveryAddress string
	CertDir          string
}

// GenerateBootstrap generates the bootstrap structure for gRPC XDS integration.
func GenerateBootstrap(opts GenerateBootstrapOptions) (*Bootstrap, error) {
	xdsMeta, err := extractMeta(opts.Node)
	if err != nil {
		return nil, fmt.Errorf("failed extracting xds metadata: %v", err)
	}

	// TODO direct to CP should use secure channel (most likely JWT + TLS, but possibly allow mTLS)
	serverURI := opts.DiscoveryAddress
	if opts.XdsUdsPath != "" {
		serverURI = fmt.Sprintf("unix:///%s", opts.XdsUdsPath)
	}

	bootstrap := Bootstrap{
		XDSServers: []XdsServer{{
			ServerURI: serverURI,
			// connect locally via agent
			ChannelCreds:   []ChannelCreds{{Type: "insecure"}},
			ServerFeatures: []string{"xds_v3"},
		}},
		Node: &corev3.Node{
			Id:       opts.Node.ID,
			Locality: opts.Node.Locality,
			Metadata: xdsMeta,
		},
		ServerListenerNameTemplate: ServerListenerNameTemplate,
	}

	if opts.CertDir != "" {
		// TODO use a more appropriate interval
		refresh, err := protomarshal.Marshal(durationpb.New(15 * time.Minute))
		if err != nil {
			return nil, err
		}

		bootstrap.CertProviders = map[string]CertificateProvider{
			"default": {
				PluginName: "file_watcher",
				Config: FileWatcherCertProviderConfig{
					PrivateKeyFile:    path.Join(opts.CertDir, "key.pem"),
					CertificateFile:   path.Join(opts.CertDir, "cert-chain.pem"),
					CACertificateFile: path.Join(opts.CertDir, "root-cert.pem"),
					RefreshDuration:   refresh,
				},
			},
		}
	}

	return &bootstrap, err
}

func extractMeta(node *model.Node) (*structpb.Struct, error) {
	bytes, err := json.Marshal(node.Metadata)
	if err != nil {
		return nil, err
	}
	rawMeta := map[string]any{}
	if err := json.Unmarshal(bytes, &rawMeta); err != nil {
		return nil, err
	}
	xdsMeta, err := structpb.NewStruct(rawMeta)
	if err != nil {
		return nil, err
	}
	return xdsMeta, nil
}

// GenerateBootstrapFile generates and writes atomically as JSON to the given file path.
func GenerateBootstrapFile(opts GenerateBootstrapOptions, path string) (*Bootstrap, error) {
	bootstrap, err := GenerateBootstrap(opts)
	if err != nil {
		return nil, err
	}
	jsonData, err := json.MarshalIndent(bootstrap, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := file.AtomicWrite(path, jsonData, os.FileMode(0o644)); err != nil {
		return nil, fmt.Errorf("failed writing to %s: %v", path, err)
	}
	return bootstrap, nil
}
