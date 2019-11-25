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

package platform

import (
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
)

const (
	AWSRegion           = "aws_region"
	AWSAvailabilityZone = "aws_availability_zone"
	AWSInstanceID       = "aws_instance_id"
	AWSAccountID        = "aws_account_id"
)

// IsAWS returns whether or not the platform for bootstrapping is Amazon Web Services.
func IsAWS() bool {
	if client := getEC2MetadataClient(); client != nil {
		return client.Available()
	}
	return false
}

type awsEnv struct {
	identity ec2metadata.EC2InstanceIdentityDocument
}

// NewAWS returns a platform environment customized for AWS.
// Metadata returned by the AWS Environment is taken from the EC2 metadata
// service.
func NewAWS() Environment {
	client := getEC2MetadataClient()
	if client == nil {
		return &awsEnv{}
	}

	doc, _ := client.GetInstanceIdentityDocument()
	return &awsEnv{
		identity: doc,
	}
}

func (a *awsEnv) Metadata() map[string]string {
	md := map[string]string{}
	if len(a.identity.AccountID) > 0 {
		md[AWSAccountID] = a.identity.AccountID
	}
	if len(a.identity.AvailabilityZone) > 0 {
		md[AWSAvailabilityZone] = a.identity.AvailabilityZone
	}
	if len(a.identity.InstanceID) > 0 {
		md[AWSInstanceID] = a.identity.InstanceID
	}
	if len(a.identity.Region) > 0 {
		md[AWSRegion] = a.identity.Region
	}
	return md
}

func (a *awsEnv) Locality() *core.Locality {
	return &core.Locality{
		Zone:   a.identity.AvailabilityZone,
		Region: a.identity.Region,
	}
}

func getEC2MetadataClient() *ec2metadata.EC2Metadata {
	sess, err := session.NewSession()
	if err != nil {
		return nil
	}
	return ec2metadata.New(sess)
}
