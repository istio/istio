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

package platform

import (
	"io/ioutil"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

const (
	AWSRegion           = "aws_region"
	AWSAvailabilityZone = "aws_availability_zone"
	AWSInstanceID       = "aws_instance_id"
	AWSAccountID        = "aws_account_id"
)

// IsAWS returns whether or not the platform for bootstrapping is Amazon Web Services.
func IsAWS() bool {
	if !systemInfoSuggestsAWS() {
		// fail-fast for local cases
		// WARN: this may lead to some cases of false negatives.
		return false
	}

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

func (a *awsEnv) Labels() map[string]string {
	return map[string]string{}
}

func (a *awsEnv) IsKubernetes() bool {
	return true
}

func getEC2MetadataClient() *ec2metadata.EC2Metadata {
	sess, err := session.NewSession(&aws.Config{
		// eliminate retries to prevent 20s wait for Available() on non-aws platforms.
		MaxRetries: aws.Int(0),
	})
	if err != nil {
		return nil
	}
	return ec2metadata.New(sess)
}

// Provides a quick way to tell if a host is likely on AWS, as the `Available()`
// check on the client is potentially very slow.
//
// Approach derived from the following:
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/identify_ec2_instances.html
// https://wiki.liutyi.info/pages/viewpage.action?pageId=7930129
// https://github.com/banzaicloud/satellite/blob/master/providers/aws.go#L28
//
// Note: avoided importing the satellite package directly to reduce number of
// dependencies, etc., required.
func systemInfoSuggestsAWS() bool {
	hypervisorUUIDBytes, _ := ioutil.ReadFile("/sys/hypervisor/uuid")
	hypervisorUUID := strings.ToLower(string(hypervisorUUIDBytes))

	productUUIDBytes, _ := ioutil.ReadFile("/sys/class/dmi/id/product_uuid")
	productUUID := strings.ToLower(string(productUUIDBytes))

	hasEC2Prefix := strings.HasPrefix(hypervisorUUID, "ec2") || strings.HasPrefix(productUUID, "ec2")

	version, _ := ioutil.ReadFile("/sys/class/dmi/id/product_version")
	hasAmazonProductVersion := strings.Contains(string(version), "amazon")

	return hasEC2Prefix || hasAmazonProductVersion
}
