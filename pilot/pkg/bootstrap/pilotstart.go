// Copyright 2019 Istio Authors
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

package bootstrap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"path"
	"reflect"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/external"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	// DefaultMCPMaxMsgSize is the default maximum message size
	DefaultMCPMaxMsgSize = 1024 * 1024 * 4
)

var (
	// DNSCertDir is the location to save generated DNS certificates.
	// TODO: we can probably avoid saving, but will require deeper changes.
	DNSCertDir = "./var/run/secrets/istio-dns"
)

// InitDiscovery is called after NewIstiod, will initialize the discovery services and
// discovery server.
func (s *Server) InitDiscovery() error {
	// Wrap the config controller with a cache.
	configController, err := configaggregate.MakeCache(s.ConfigStores)
	if err != nil {
		return err
	}

	// Update the config controller
	s.ConfigController = configController
	// Create the config store.
	s.IstioConfigStore = model.MakeIstioStore(s.ConfigController)

	s.ServiceController = aggregate.NewController()
	// Defer running of the service controllers until Start is called, init may add more.
	s.addStartFunc(func(stop <-chan struct{}) error {
		go s.ServiceController.Run(stop)
		return nil
	})

	// ServiceEntry from config and aggregate discovery in s.ServiceController
	// This will use the istioConfigStore and ConfigController.
	s.addConfig2ServiceEntry()

	return nil
}

func (s *Server) WaitStop(stop <-chan struct{}) {
	<-stop
	// TODO: add back	if needed:	authn_model.JwtKeyResolver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.HttpServer.Shutdown(ctx)
	if err != nil {
		log.Warna(err)
	}
	if s.Args.ForceStop {
		s.GrpcServer.Stop()
	} else {
		s.GrpcServer.GracefulStop()
	}
}

func (s *Server) Serve(stop <-chan struct{}) {

	go func() {
		if err := s.HttpServer.Serve(s.HttpListener); err != nil {
			log.Warna(err)
		}
	}()
	go func() {
		if err := s.GrpcServer.Serve(s.GrpcListener); err != nil {
			log.Warna(err)
		}
	}()
	go func() {
		if err := s.SecureGRPCServer.Serve(s.SecureGrpcListener); err != nil {
			log.Warna(err)
		}
	}()
}

func defaultMeshConfig() *meshconfig.MeshConfig {
	meshConfigObj := mesh.DefaultMeshConfig()
	meshConfig := &meshConfigObj

	meshConfig.SdsUdsPath = "unix:/etc/istio/proxy/SDS"
	meshConfig.EnableSdsTokenMount = true

	// TODO: Agent should use /var/lib/istio/proxy - this is under $HOME for istio, not in etc. Not running as root.

	return meshConfig
}

// WatchMeshConfig creates the mesh in the pilotConfig from the input arguments.
// Will set s.Mesh, and keep it updated.
// On change, ConfigUpdate will be called.
// TODO: merge with user-specified mesh config.
func (s *Server) WatchMeshConfig(args string) error {
	var meshConfig *meshconfig.MeshConfig
	var err error

	// Mesh config is required - this is the primary source of config.
	meshConfig, err = cmd.ReadMeshConfig(args)
	if err != nil {
		log.Infof("No local mesh config found, using defaults")
		meshConfig = defaultMeshConfig()
	}

	// Watch the config file for changes and reload if it got modified
	s.addFileWatcher(args, func() {
		// Reload the config file
		meshConfig, err = cmd.ReadMeshConfig(args)
		if err != nil {
			log.Warnf("failed to read mesh configuration, using default: %v", err)
			return
		}
		if !reflect.DeepEqual(meshConfig, s.Mesh) {
			log.Infof("mesh configuration updated to: %s", spew.Sdump(meshConfig))
			if !reflect.DeepEqual(meshConfig.ConfigSources, s.Mesh.ConfigSources) {
				log.Infof("mesh configuration sources have changed")
				//TODO Need to re-create or reload initConfigController()
			}
			s.Mesh = meshConfig
			s.Args.MeshConfig = meshConfig
			if s.EnvoyXdsServer != nil {
				s.EnvoyXdsServer.Env.Mesh = meshConfig
				s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{Full: true})
			}
		}
	})

	log.Infof("mesh configuration %s", spew.Sdump(meshConfig))
	log.Infof("version %s", version.Info.String())

	s.Mesh = meshConfig
	s.Args.MeshConfig = meshConfig
	return nil
}

// addConfig2ServiceEntry creates and initializes the ServiceController used for translating
// ServiceEntries from config store to discovery.
func (s *Server) addConfig2ServiceEntry() {
	serviceEntryStore := external.NewServiceDiscovery(s.ConfigController, s.IstioConfigStore)

	// add service entry registry to aggregator by default
	serviceEntryRegistry := aggregate.Registry{
		Name:             "ServiceEntries",
		Controller:       serviceEntryStore,
		ServiceDiscovery: serviceEntryStore,
	}
	s.ServiceController.AddRegistry(serviceEntryRegistry)
}

// initialize secureGRPCServer - using K8S DNS certs
func (s *Server) initSecureGrpcServerDNS(options *istiokeepalive.Options) error {
	certDir := DNSCertDir

	key := path.Join(certDir, constants.KeyFilename)
	cert := path.Join(certDir, constants.CertChainFilename)

	tlsCreds, err := credentials.NewServerTLSFromFile(cert, key)
	// certs not ready yet.
	if err != nil {
		return err
	}

	// TODO: parse the file to determine expiration date. Restart listener before expiration
	certificate, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}

	opts := s.grpcServerOptions(options)
	opts = append(opts, grpc.Creds(tlsCreds))
	s.SecureGRPCServerDNS = grpc.NewServer(opts...)
	s.EnvoyXdsServer.Register(s.SecureGRPCServerDNS)

	s.SecureHTTPServerDNS = &http.Server{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{certificate},
			VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				// For now accept any certs - pilot is not authenticating the caller, TLS used for
				// privacy
				return nil
			},
			NextProtos: []string{"h2", "http/1.1"},
			ClientAuth: tls.NoClientCert, // auth will be based on JWT token signed by K8S
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(
				r.Header.Get("Content-Type"), "application/grpc") {
				s.SecureGRPCServer.ServeHTTP(w, r)
			} else {
				s.Mux.ServeHTTP(w, r)
			}
		}),
	}

	return nil
}
