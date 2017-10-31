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

	"github.com/fullsailor/pkcs7"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"istio.io/istio/auth/pkg/pki"
)

const (
	// AWSCertificatePem is the official public certificate for AWS
	// copied from https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instance-identity-documents.html
	AWSCertificatePem = `-----BEGIN CERTIFICATE-----
MIIC7TCCAq0CCQCWukjZ5V4aZzAJBgcqhkjOOAQDMFwxCzAJBgNVBAYTAlVTMRkw
FwYDVQQIExBXYXNoaW5ndG9uIFN0YXRlMRAwDgYDVQQHEwdTZWF0dGxlMSAwHgYD
VQQKExdBbWF6b24gV2ViIFNlcnZpY2VzIExMQzAeFw0xMjAxMDUxMjU2MTJaFw0z
ODAxMDUxMjU2MTJaMFwxCzAJBgNVBAYTAlVTMRkwFwYDVQQIExBXYXNoaW5ndG9u
IFN0YXRlMRAwDgYDVQQHEwdTZWF0dGxlMSAwHgYDVQQKExdBbWF6b24gV2ViIFNl
cnZpY2VzIExMQzCCAbcwggEsBgcqhkjOOAQBMIIBHwKBgQCjkvcS2bb1VQ4yt/5e
ih5OO6kK/n1Lzllr7D8ZwtQP8fOEpp5E2ng+D6Ud1Z1gYipr58Kj3nssSNpI6bX3
VyIQzK7wLclnd/YozqNNmgIyZecN7EglK9ITHJLP+x8FtUpt3QbyYXJdmVMegN6P
hviYt5JH/nYl4hh3Pa1HJdskgQIVALVJ3ER11+Ko4tP6nwvHwh6+ERYRAoGBAI1j
k+tkqMVHuAFcvAGKocTgsjJem6/5qomzJuKDmbJNu9Qxw3rAotXau8Qe+MBcJl/U
hhy1KHVpCGl9fueQ2s6IL0CaO/buycU1CiYQk40KNHCcHfNiZbdlx1E9rpUp7bnF
lRa2v1ntMX3caRVDdbtPEWmdxSCYsYFDk4mZrOLBA4GEAAKBgEbmeve5f8LIE/Gf
MNmP9CM5eovQOGx5ho8WqD+aTebs+k2tn92BBPqeZqpWRa5P/+jrdKml1qx4llHW
MXrs3IgIb6+hUIB+S8dz8/mmO0bpr76RoZVCXYab2CZedFut7qc3WUH9+EUAH5mw
vSeDCOUMYQR7R9LINYwouHIziqQYMAkGByqGSM44BAMDLwAwLAIUWXBlk40xTwSw
7HX32MxXYruse9ACFBNGmdX2ZBrVNGrN9N2f6ROk0k9K
-----END CERTIFICATE-----`
)

// AwsClientImpl is the implementation of AWS metadata client.
type AwsClientImpl struct {
	client *ec2metadata.EC2Metadata
}

// NewAwsClientImpl creates a new AwsClientImpl.
func NewAwsClientImpl() *AwsClientImpl {
	return &AwsClientImpl{ec2metadata.New(session.Must(session.NewSession()))}
}

// GetDialOptions returns the GRPC dial options to connect to the CA.
func (ci *AwsClientImpl) GetDialOptions(cfg *ClientConfig) ([]grpc.DialOption, error) {
	creds, err := credentials.NewClientTLSFromFile(cfg.RootCACertFile, "")
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
	cert, err := pki.ParsePemEncodedCertificate([]byte(AWSCertificatePem))
	if err != nil {
		return nil, fmt.Errorf("Failed to parse AWS public certificate: %v", err)
	}

	resp, err := ci.client.GetDynamicData("instance-identity/pkcs7")
	if err != nil {
		return nil, fmt.Errorf("Failed to get EC2 instance PKCS7 signature: %v", err)
	}

	dec, err := base64.StdEncoding.DecodeString(resp)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode PKCS7 signature: %v", err)
	}

	parsed, err := pkcs7.Parse(dec)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse PKCS7 response: %v", err)
	}

	parsed.Certificates = []*x509.Certificate{cert}
	if err := parsed.Verify(); err != nil {
		return nil, fmt.Errorf("Failed to verify PKCS7 signature: %v", err)
	}

	return parsed.Content, nil
}

// GetAgentCredential retrieves the instance identity document as the
// agent credential used by node agent
func (ci *AwsClientImpl) GetAgentCredential() ([]byte, error) {
	doc, err := ci.getInstanceIdentityDocument()
	if err != nil {
		return nil, fmt.Errorf("Failed to get EC2 instance identity document: %v", err)
	}

	bytes, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal identity document %v: %v", doc, err)
	}

	return bytes, nil
}

// GetCredentialType returns the credential type as "aws".
func (ci *AwsClientImpl) GetCredentialType() string {
	return "aws"
}
