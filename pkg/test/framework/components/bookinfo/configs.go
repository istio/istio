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

package bookinfo

import (
	"fmt"
	"path"
	"strings"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
)

// ConfigFile represents config yaml files for different bookinfo scenarios.
type ConfigFile string

const (
	// NetworkingBookinfoGateway uses "networking/bookinfo-gateway.yaml"
	NetworkingBookinfoGateway ConfigFile = "networking/bookinfo-gateway.yaml"

	// NetworkingDestinationRuleAll uses "networking/destination-rule-all.yaml"
	NetworkingDestinationRuleAll ConfigFile = "networking/destination-rule-all.yaml"

	// NetworkingDestinationRuleAllMtls uses "networking/destination-rule-all-mtls.yaml"
	NetworkingDestinationRuleAllMtls ConfigFile = "networking/destination-rule-all-mtls.yaml"

	// NetworkingVirtualServiceAllV1 uses "networking/virtual-service-all-v1.yaml"
	NetworkingVirtualServiceAllV1 ConfigFile = "networking/virtual-service-all-v1.yaml"

	// NetworkingTcpDbRule uses "networking/virtual-service-ratings-db.yaml"
	NetworkingTCPDbRule ConfigFile = "networking/virtual-service-ratings-db.yaml"

	// NetworkingReviewsV3Rule uses "networking/virtual-service-reviews-v3"
	NetworkingReviewsV3Rule ConfigFile = "networking/virtual-service-reviews-v3.yaml"

	// RatingsRedisRateLimitFixed uses Mixer configs
	RatingsRedisRateLimitFixed ConfigFile = "policy/mixer-rule-ratings-redis-quota-fixed-window.yaml"

	// RatingsRedisRateLimitRolling uses Mixer configs
	RatingsRedisRateLimitRolling ConfigFile = "policy/mixer-rule-ratings-redis-quota-rolling-window.yaml"

	// ProductPageRedisRateLimit uses Mixer configs
	ProductPageRedisRateLimit ConfigFile = "policy/mixer-rule-productpage-redis-quota-fixed-window.yaml"

	// PolicyDenyIPRule uses "policy/mixer-rule-deny-ip.yaml"
	PolicyDenyIPRule ConfigFile = "policy/mixer-rule-deny-ip.yaml"

	// TelemetryLogEntry uses "telemetry/log-entry.yaml"
	TelemetryLogEntry ConfigFile = "telemetry/log-entry.yaml"

	// DoubleMetric uses "telemetry/metrics.yaml"
	DoubleMetric ConfigFile = "telemetry/metrics.yaml"
)

// LoadGatewayFileWithNamespaceOrFail loads a Book Info Gateway configuration file from the system, changes it to be fit
// for the namespace provided and returns its contents.
func (l ConfigFile) LoadGatewayFileWithNamespaceOrFail(t test.Failer, namespace string) string {
	t.Helper()

	content, err := l.LoadGatewayFileWithNamespace(namespace)
	if err != nil {
		t.Fatalf("err:%v", err)
	}

	return content
}

// LoadWithNamespaceOrFail loads a Book Info configuration file from the systemchanges it to be fit
// for the namespace provided and returns its contents.
func (l ConfigFile) LoadWithNamespaceOrFail(t test.Failer, namespace string) string {
	t.Helper()

	content, err := l.LoadWithNamespace(namespace)
	if err != nil {
		t.Fatalf("err:%v", err)
	}

	return content
}

// LoadGatewayFileWithNamespaceOrFail loads a Book Info Gateway configuration file from the system, changes it to be fit
// for the namespace provided and returns its contents.
func (l ConfigFile) LoadGatewayFileWithNamespace(namespace string) (string, error) {
	content, err := l.LoadWithNamespace(namespace)
	if err != nil {
		return "", err
	}
	if namespace != "" {
		content = replaceGatewayAndHostAddressWithNamespace(content, namespace)
	}
	return content, nil
}

// LoadWithNamespaceOrFail loads a Book Info configuration file from the systemchanges it to be fit
// for the namespace provided and returns its contents.
func (l ConfigFile) LoadWithNamespace(namespace string) (string, error) {
	p := path.Join(env.BookInfoRoot, string(l))

	content, err := file.AsString(p)
	if err != nil {
		return "", fmt.Errorf("unable to load config %s at %v, err:%v", l, p, err)
	}

	scopes.Framework.Debugf("Loaded BookInfo file: %s\n%s\n", p, content)
	if namespace != "" {
		content = replaceBookinfoAppAddressWithFQDNAddress(content, namespace)
	}
	return content, nil
}

// LoadOrFail loads a Book Info configuration file from the system and returns its contents.
func (l ConfigFile) LoadOrFail(t test.Failer) string {
	t.Helper()
	return l.LoadWithNamespaceOrFail(t, "")
}

func GetDestinationRuleConfigFileOrFail(t test.Failer, ctx resource.Context) ConfigFile {
	t.Helper()

	cfg, err := GetDestinationRuleConfigFile(ctx)
	if err != nil {
		t.Fatalf("bookinfo.GetDestinationRuleConfigFile: %v", err)
	}

	return cfg
}

func GetDestinationRuleConfigFile(ctx resource.Context) (ConfigFile, error) {
	return NetworkingDestinationRuleAllMtls, nil
}

func replaceBookinfoAppAddressWithFQDNAddress(fileContent, namespace string) string {
	content := fileContent
	content = strings.Replace(content, "host: productpage", "host: productpage."+namespace+".svc.cluster.local", -1)
	content = strings.Replace(content, "host: reviews", "host: reviews."+namespace+".svc.cluster.local", -1)
	content = strings.Replace(content, "host: ratings", "host: ratings."+namespace+".svc.cluster.local", -1)
	content = strings.Replace(content, "host: details", "host: details."+namespace+".svc.cluster.local", -1)
	content = strings.Replace(content, "- productpage", "- productpage."+namespace+".svc.cluster.local", -1)
	content = strings.Replace(content, "- reviews", "- reviews."+namespace+".svc.cluster.local", -1)
	content = strings.Replace(content, "- ratings", "- ratings."+namespace+".svc.cluster.local", -1)
	content = strings.Replace(content, "- details", "- details."+namespace+".svc.cluster.local", -1)
	return content
}

func replaceGatewayAndHostAddressWithNamespace(fileContent, namespace string) string {
	content := fileContent
	content = strings.Replace(content, "- bookinfo-gateway", "- "+namespace+"/bookinfo-gateway", -1)
	return content
}
