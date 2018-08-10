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

package platform

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/security/pkg/pki/util"
)

const (
	// AWSCertificatePem is the official public RSA certificate for AWS
	AWSCertificatePem = `-----BEGIN CERTIFICATE-----
MIIDIjCCAougAwIBAgIJAKnL4UEDMN/FMA0GCSqGSIb3DQEBBQUAMGoxCzAJBgNV
BAYTAlVTMRMwEQYDVQQIEwpXYXNoaW5ndG9uMRAwDgYDVQQHEwdTZWF0dGxlMRgw
FgYDVQQKEw9BbWF6b24uY29tIEluYy4xGjAYBgNVBAMTEWVjMi5hbWF6b25hd3Mu
Y29tMB4XDTE0MDYwNTE0MjgwMloXDTI0MDYwNTE0MjgwMlowajELMAkGA1UEBhMC
VVMxEzARBgNVBAgTCldhc2hpbmd0b24xEDAOBgNVBAcTB1NlYXR0bGUxGDAWBgNV
BAoTD0FtYXpvbi5jb20gSW5jLjEaMBgGA1UEAxMRZWMyLmFtYXpvbmF3cy5jb20w
gZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJAoGBAIe9GN//SRK2knbjySG0ho3yqQM3
e2TDhWO8D2e8+XZqck754gFSo99AbT2RmXClambI7xsYHZFapbELC4H91ycihvrD
jbST1ZjkLQgga0NE1q43eS68ZeTDccScXQSNivSlzJZS8HJZjgqzBlXjZftjtdJL
XeE4hwvo0sD4f3j9AgMBAAGjgc8wgcwwHQYDVR0OBBYEFCXWzAgVyrbwnFncFFIs
77VBdlE4MIGcBgNVHSMEgZQwgZGAFCXWzAgVyrbwnFncFFIs77VBdlE4oW6kbDBq
MQswCQYDVQQGEwJVUzETMBEGA1UECBMKV2FzaGluZ3RvbjEQMA4GA1UEBxMHU2Vh
dHRsZTEYMBYGA1UEChMPQW1hem9uLmNvbSBJbmMuMRowGAYDVQQDExFlYzIuYW1h
em9uYXdzLmNvbYIJAKnL4UEDMN/FMAwGA1UdEwQFMAMBAf8wDQYJKoZIhvcNAQEF
BQADgYEAFYcz1OgEhQBXIwIdsgCOS8vEtiJYF+j9uO6jz7VOmJqO+pRlAbRlvY8T
C1haGgSI/A1uZUKs/Zfnph0oEI0/hu1IIJ/SKBDtN5lvmZ/IzbOPIJWirlsllQIQ
7zvWbGd9c9+Rm3p04oTvhup99la7kZqevJK0QRdD/6NpCKsqP/0=
-----END CERTIFICATE-----`
)

// AwsClientImpl is the implementation of AWS metadata client.
type AwsClientImpl struct {
	// Root CA cert file to validate the gRPC service in CA.
	rootCertFile string

	client *ec2metadata.EC2Metadata
}

// NewAwsClientImpl creates a new AwsClientImpl.
func NewAwsClientImpl(rootCert string) *AwsClientImpl {
	return &AwsClientImpl{
		rootCertFile: rootCert,
		client:       ec2metadata.New(session.Must(session.NewSession())),
	}
}

// GetDialOptions returns the GRPC dial options to connect to the CA.
func (ci *AwsClientImpl) GetDialOptions() ([]grpc.DialOption, error) {
	creds, err := credentials.NewClientTLSFromFile(ci.rootCertFile, CitadelDNSSan)
	if err != nil {
		return nil, err
	}

	options := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	return options, nil
}

// IsProperPlatform returns whether the AWS platform client is available.
func (ci *AwsClientImpl) IsProperPlatform() bool {
	return ci.client.Available()
}

// GetServiceIdentity extracts service identity from userdata. This function should be
// pluggable for different AWS deployments in the future.
func (ci *AwsClientImpl) GetServiceIdentity() (string, error) {
	return "", nil
}

func (ci *AwsClientImpl) getInstanceIdentityDocument() ([]byte, error) {
	cert, err := util.ParsePemEncodedCertificate([]byte(AWSCertificatePem))
	if err != nil {
		return nil, fmt.Errorf("failed to parse AWS public certificate: %v", err)
	}

	doc, err := ci.client.GetDynamicData("instance-identity/document")
	if err != nil {
		return nil, fmt.Errorf("failed to get EC2 instance identity document: %v", err)
	}

	resp, err := ci.client.GetDynamicData("instance-identity/signature")
	if err != nil {
		return nil, fmt.Errorf("failed to get EC2 instance identity signature: %v", err)
	}

	dec, err := base64.StdEncoding.DecodeString(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to decode EC2 instance identity signature: %v", err)
	}

	if err := cert.CheckSignature(x509.SHA256WithRSA, []byte(doc), dec); err != nil {
		return nil, fmt.Errorf("failed to verify PKCS7 signature: %v", err)
	}

	return []byte(doc), nil
}

// GetAgentCredential retrieves the instance identity document as the
// agent credential used by node agent
func (ci *AwsClientImpl) GetAgentCredential() ([]byte, error) {
	doc, err := ci.getInstanceIdentityDocument()
	if err != nil {
		return nil, fmt.Errorf("failed to get EC2 instance identity document: %v", err)
	}

	bytes, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal identity document %v: %v", doc, err)
	}

	return bytes, nil
}

// GetCredentialType returns the credential type as "aws".
func (ci *AwsClientImpl) GetCredentialType() string {
	return "aws"
}
