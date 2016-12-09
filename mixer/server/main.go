// Copyright 2016 Google Inc.
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

package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"io/ioutil"
	"strings"

	"github.com/golang/glog"

	"istio.io/mixer/adapters"
	"istio.io/mixer/adapters/factMapper"
)

func main() {
	const (
		// GRPCPort -- default gRPC server port
		GRPCPort = 9091
		// MaxMessageSize -- default gRPC maximum message size
		MaxMessageSize = 1 * 1024 * 1024
		// MaxConcurrentStreams -- default gRPC concurrency factor
		MaxConcurrentStreams = 2
		// CompressedPayload -- default of whether or not to use compressed gRPC payloads
		CompressedPayload = false
	)

	grpcPort := flag.Int("grpcPort", GRPCPort, "Port exposed for Mixer gRPC API")
	maxMessageSize := flag.Uint("maxMessageSize", MaxMessageSize, "Maximum size of individual gRPC messages")
	maxConcurrentStreams := flag.Uint("maxConcurrentStreams", MaxConcurrentStreams, "Maximum supported number of concurrent gRPC streams")
	compressedPayload := flag.Bool("compressedPayload", CompressedPayload, "Whether to compress gRPC messages")
	serverCertFile := flag.String("serverCertFile", "", "The TLS cert file")
	serverKeyFile := flag.String("serverKeyFile", "", "The TLS key file")
	clientCertFiles := flag.String("clientCertFiles", "", "A set of comma-separated client X509 cert files")
	flag.Parse()

	var err error
	var serverCert *tls.Certificate
	var clientCerts *x509.CertPool

	if *serverCertFile != "" && *serverKeyFile != "" {
		sc, err := tls.LoadX509KeyPair(*serverCertFile, *serverKeyFile)
		if err != nil {
			glog.Exitf("Failed to load server certificate and server key: %v", err)
		}
		serverCert = &sc
	}

	if *clientCertFiles != "" {
		clientCerts = x509.NewCertPool()
		for _, clientCertFile := range strings.Split(*clientCertFiles, ",") {
			pem, err := ioutil.ReadFile(clientCertFile)
			if err != nil {
				glog.Exitf("Failed to load client certificate: %v", err)
			}
			clientCerts.AppendCertsFromPEM(pem)
		}
	}

	var adapterMgr *AdapterManager
	if adapterMgr, err = NewAdapterManager(); err != nil {
		glog.Exitf("Unable to initialize adapters: %v", err)
	}

	// TODO: hackily create a fact mapper builder & adapter.
	// This necessarily needs to be discovered & created through normal
	// adapter config goo, but that doesn't exist yet
	rules := make(map[string]string)
	rules["Lab1"] = "Fact1|Fact2"
	builder := adapterMgr.FactConverters["FactMapper"]
	var adapter adapters.Adapter
	adapter, err = builder.NewAdapter(&factMapper.AdapterConfig{Rules: rules})
	if err != nil {
		glog.Exitf("Unable to create fact conversion adapter " + err.Error())
	}
	factConverter := adapter.(adapters.FactConverter)

	apiServerOptions := APIServerOptions{
		Port:                 uint16(*grpcPort),
		MaxMessageSize:       *maxMessageSize,
		MaxConcurrentStreams: *maxConcurrentStreams,
		CompressedPayload:    *compressedPayload,
		ServerCertificate:    serverCert,
		ClientCertificates:   clientCerts,
		Handlers:             NewAPIHandlers(),
		FactConverter:        factConverter,
	}

	glog.Infof("Starting gRPC server on port %v", apiServerOptions.Port)
	var apiServer *APIServer
	if apiServer, err = NewAPIServer(&apiServerOptions); err != nil {
		glog.Exitf("Unable to initialize API server " + err.Error())
	}
	apiServer.Start()
}
