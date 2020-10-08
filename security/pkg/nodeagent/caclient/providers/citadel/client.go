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

package caclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc/metadata"

	"istio.io/api/networking/v1alpha3"
	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/pki/signingapi"
	"istio.io/pkg/log"
)

const (
	caServerName      = "istio-citadel"
	bearerTokenPrefix = "Bearer "
	requestTimeout    = 20 * time.Second // seconds
)

var (
	citadelClientLog = log.RegisterScope("citadelclient", "citadel client debugging", 0)
)

type citadelClient struct {
	caEndpoint string
	client     *signingapi.Client
	secOpts    *security.Options
}

// NewCitadelClient create a CA client for Citadel.
func NewCitadelClient(endpoint string, tls bool, rootCert string, secOpts *security.Options) (security.Client, error) {
	clientTLSSettings := &v1alpha3.ClientTLSSettings{
		CaCertificates: rootCert,
	}

	if tls {
		clientTLSSettings.Mode = v1alpha3.ClientTLSSettings_SIMPLE
	} else {
		clientTLSSettings.Mode = v1alpha3.ClientTLSSettings_DISABLE
	}

	// Initial implementation of citadel hardcoded the SAN to 'istio-citadel'. For backward compat, keep it.
	// TODO: remove this once istiod replaces citadel.
	// External CAs will use their normal server names.
	if strings.Contains(endpoint, "citadel") {
		clientTLSSettings.Sni = caServerName
	}
	// For debugging on localhost (with port forward)
	// TODO: remove once istiod is stable and we have a way to validate JWTs locally
	if strings.Contains(endpoint, "localhost") {
		clientTLSSettings.Sni = "istiod.istio-system.svc"
	}

	// Connect with pre provisioning TLS Client Certificates
	if secOpts.ProvCert != "" {
		clientTLSSettings.PrivateKey = path.Join(secOpts.ProvCert, "key.pem")
		clientTLSSettings.ClientCertificate = path.Join(secOpts.ProvCert, "cert-chain.pem")
		clientTLSSettings.Mode = v1alpha3.ClientTLSSettings_MUTUAL
	}

	signingAPIClient := signingapi.New(endpoint, clientTLSSettings, requestTimeout)

	c := &citadelClient{
		caEndpoint: endpoint,
		client:     signingAPIClient,
		secOpts:    secOpts,
	}

	err := c.client.Connect(nil)
	if err != nil {
		citadelClientLog.Errorf("Failed to connect to endpoint %s: %v", endpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %s", endpoint)
	}
	return c, nil
}

// CSR Sign calls Citadel to sign a CSR.
func (c *citadelClient) CSRSign(ctx context.Context, reqID string, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {

	// Opaque metadata provided by the XDS node to Istio
	csrMetadata := map[string]interface{}{
		"ClusterID":    c.secOpts.ClusterID,
		"WorkloadIPs":  c.secOpts.WorkloadIPs,
		"WorkloadName": c.secOpts.WorkloadName,
	}

	metaStruct, err := mapToStruct(csrMetadata)
	if err != nil {
		citadelClientLog.Errorf("parse CSR Metadata for citadel failed: %v", err)
		return nil, fmt.Errorf("parse CSR Metadata for citadel failed: %v", err)
	}
	req := &pb.IstioCertificateRequest{
		Csr:              string(csrPEM),
		Metadata:         metaStruct,
		ValidityDuration: certValidTTLInSec,
	}
	if token != "" {
		// add Bearer prefix, which is required by Citadel.
		token = bearerTokenPrefix + token
		// keep cluster ID in header's metadata for backward compatibility
		ctx = metadata.NewOutgoingContext(ctx,
			metadata.Pairs("Authorization", token, "ClusterID", c.secOpts.ClusterID))
	} else {
		// This may use the per call credentials, if enabled.
		err := c.reconnect()
		if err != nil {
			citadelClientLog.Errorf("Failed to Reconnect: %v", err)
			return nil, err
		}
	}

	resp, err := c.client.CreateCertificate(ctx, req)
	if err != nil {
		citadelClientLog.Errorf("Failed to create certificate: %v", err)
		return nil, err
	}

	return resp.CertChain, nil
}

func (c *citadelClient) reconnect() error {
	if c.client.IsConnected() {
		if err := c.client.Conn.Close(); err != nil {
			return fmt.Errorf("failed to close connection")
		}
	}

	if err := c.client.Connect(nil); err != nil {
		return fmt.Errorf("create citadel client failed: %v", err)
	}

	return nil
}

func mapToStruct(msg map[string]interface{}) (*types.Struct, error) {
	jb, err := json.Marshal(msg)
	if err != nil {
		citadelClientLog.Errorf("parse Metadata %#v to Json failed: %v", msg, err)
		return nil, fmt.Errorf("parse Metadata Map[string]interface{} to Json failed: %v", err)
	}
	jbu := jsonpb.Unmarshaler{AllowUnknownFields: true}
	pb := &types.Struct{}
	err = jbu.Unmarshal(bytes.NewReader(jb), pb)
	if err != nil {
		citadelClientLog.Errorf("parse Metadata JSON to proto.struct failed: %v", err)
		return nil, fmt.Errorf("parse Metadata JSON to proto.struct failed: %v", err)
	}
	return pb, nil
}
