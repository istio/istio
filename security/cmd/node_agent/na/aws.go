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

package na

import (
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type awsPlatformImpl struct {
	client *ec2metadata.EC2Metadata
}

func (na *awsPlatformImpl) GetDialOptions(cfg *Config) ([]grpc.DialOption, error) {
	creds, err := credentials.NewClientTLSFromFile(cfg.RootCACertFile, "")
	if err != nil {
		return nil, err
	}

	options := []grpc.DialOption{grpc.WithTransportCredentials(creds)}
	return options, nil
}

func (na *awsPlatformImpl) IsProperPlatform() bool {
	return na.client.Available()
}

// Extract service identity from userdata. This function should be
// pluggable for different AWS deployments in the future.
func (na *awsPlatformImpl) GetServiceIdentity() (string, error) {
	return "", nil
}

// GetAgentCredential retrieves the instance identity document as the
// agent credential used by node agent
func (na *awsPlatformImpl) GetAgentCredential() ([]byte, error) {
	doc, err := na.client.GetInstanceIdentityDocument()
	if err != nil {
		return []byte{}, fmt.Errorf("Failed to get EC2 instance identity document: %v", err)
	}

	bytes, err := json.Marshal(doc)
	if err != nil {
		return []byte{}, fmt.Errorf("Failed to marshal identity document %v: %v", doc, err)
	}

	return bytes, nil
}

func (na *awsPlatformImpl) GetCredentialType() string {
	return "aws"
}
