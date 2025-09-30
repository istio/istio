// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/brotli/compressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/gzip/compressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/compression/zstd/compressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/compressor/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/resource_monitors/downstream_connections/v3"
	_ "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/testing/protocmp"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/version"
)

type stats struct {
	prefixes string
	suffixes string
	regexps  string
}

var (
	// The following set of inclusions add minimal upstream and downstream metrics.
	// Upstream metrics record client side measurements.
	// Downstream metrics record server side measurements.
	upstreamStatsSuffixes = "upstream_rq_1xx,upstream_rq_2xx,upstream_rq_3xx,upstream_rq_4xx,upstream_rq_5xx," +
		"upstream_rq_time,upstream_cx_tx_bytes_total,upstream_cx_rx_bytes_total,upstream_cx_total"

	// example downstream metric: http.10.16.48.230_8080.downstream_rq_2xx
	// http.<pod_ip>_<port>.downstream_rq_2xx
	// This metric is collected at the inbound listener at a sidecar.
	// All the other downstream metrics at a sidecar are from the application to the local sidecar.
	downstreamStatsSuffixes = "downstream_rq_1xx,downstream_rq_2xx,downstream_rq_3xx,downstream_rq_4xx,downstream_rq_5xx," +
		"downstream_rq_time,downstream_cx_tx_bytes_total,downstream_cx_rx_bytes_total,downstream_cx_total"
)

// Generate configs for the default configs used by istio.
// If the template is updated, refresh golden files using:
// REFRESH_GOLDEN=true go test ./pkg/bootstrap/...
func TestGolden(t *testing.T) {
	cases := []struct {
		base                       string
		envVars                    map[string]string
		annotations                map[string]string
		sdsUDSPath                 string
		sdsTokenPath               string
		expectLightstepAccessToken bool
		stats                      stats
		checkLocality              bool
		stsPort                    int
		platformMeta               map[string]string
		setup                      func()
		teardown                   func()
		check                      func(got *bootstrap.Bootstrap, t *testing.T)
		compliancePolicy           string
	}{
		{
			base: "xdsproxy",
		},
		{
			base: "auth",
		},
		{
			base:         "authsds",
			sdsUDSPath:   "udspath",
			sdsTokenPath: "/var/run/secrets/tokens/istio-token",
		},
		{
			base: "default",
		},
		{
			base: "ambient",
			envVars: map[string]string{
				"ISTIO_META_ENABLE_HBONE": "true", // This is our indication that this proxy is in an ambient installation
			},
		},
		{
			base: "explicit_internal_address",
		},
		{
			base: "running",
			envVars: map[string]string{
				"ISTIO_META_ISTIO_PROXY_SHA":   "istio-proxy:sha",
				"ISTIO_META_INTERCEPTION_MODE": "REDIRECT",
				"ISTIO_META_ISTIO_VERSION":     "release-3.1",
				"ISTIO_META_POD_NAME":          "svc-0-0-0-6944fb884d-4pgx8",
				"POD_NAME":                     "svc-0-0-0-6944fb884d-4pgx8",
				"POD_NAMESPACE":                "test",
				"INSTANCE_IP":                  "10.10.10.1",
				"ISTIO_METAJSON_LABELS":        `{"version": "v1alpha1", "app": "test", "istio-locality":"regionA.zoneB.sub_zoneC"}`,
			},
			annotations: map[string]string{
				"istio.io/insecurepath": "{\"paths\":[\"/metrics\",\"/live\"]}",
			},
			checkLocality: true,
		},
		{
			base: "runningsds",
			envVars: map[string]string{
				"ISTIO_META_ISTIO_PROXY_SHA":   "istio-proxy:sha",
				"ISTIO_META_INTERCEPTION_MODE": "REDIRECT",
				"ISTIO_META_ISTIO_VERSION":     "release-3.1",
				"ISTIO_META_POD_NAME":          "svc-0-0-0-6944fb884d-4pgx8",
				"POD_NAME":                     "svc-0-0-0-6944fb884d-4pgx8",
				"POD_NAMESPACE":                "test",
				"INSTANCE_IP":                  "10.10.10.1",
				"ISTIO_METAJSON_LABELS":        `{"version": "v1alpha1", "app": "test", "istio-locality":"regionA.zoneB.sub_zoneC"}`,
			},
			annotations: map[string]string{
				"istio.io/insecurepath": "{\"paths\":[\"/metrics\",\"/live\"]}",
			},
			sdsUDSPath:    "udspath",
			sdsTokenPath:  "/var/run/secrets/tokens/istio-token",
			checkLocality: true,
		},
		{
			base:                       "tracing_lightstep",
			expectLightstepAccessToken: true,
		},
		{
			base: "tracing_zipkin",
		},
		{
			base: "tracing_datadog",
		},
		{
			base: "metrics_no_statsd",
		},
		{
			base: "tracing_none",
		},
		{
			// Specify zipkin/statsd address, similar with the default config in v1 tests
			base: "all",
		},
		{
			base: "stats_inclusion",
			annotations: map[string]string{
				"sidecar.istio.io/statsInclusionPrefixes": "prefix1,prefix2,http.{pod_ip}_",
				"sidecar.istio.io/statsInclusionSuffixes": "suffix1,suffix2" + "," + upstreamStatsSuffixes + "," + downstreamStatsSuffixes,
				"sidecar.istio.io/statsInclusionRegexps":  "http.[0-9]*\\.[0-9]*\\.[0-9]*\\.[0-9]*_8080.downstream_rq_time",
				"sidecar.istio.io/extraStatTags":          "dlp_status,dlp_error",
				"sidecar.istio.io/statsHistogramBuckets":  `{"istio":[1,5,10,50,100,500,1000,5000,10000],"envoy":[1,5,10,25,50,100,250,500,1000,2500,5000,10000]}`,
			},
			stats: stats{
				prefixes: "prefix1,prefix2,http.10.3.3.3_,http.10.4.4.4_,http.10.5.5.5_,http.10.6.6.6_",
				suffixes: "suffix1,suffix2," + upstreamStatsSuffixes + "," + downstreamStatsSuffixes,
				regexps:  "http.[0-9]*\\.[0-9]*\\.[0-9]*\\.[0-9]*_8080.downstream_rq_time",
			},
		},
		{
			base: "tracing_tls",
		},
		{
			base: "tracing_tls_custom_sni",
		},
		{
			base:             "lrs",
			compliancePolicy: "fips-140-2",
			envVars: map[string]string{
				"ISTIO_META_LOAD_STATS_CONFIG_JSON": `{"api_type": "GRPC", "transport_api_version": "V3"}`,
			},
		},
		{
			base: "stats_compression_gzip",
			annotations: map[string]string{
				"sidecar.istio.io/statsCompression": "gzip",
			},
		},
		{
			base: "stats_compression_brotli",
			annotations: map[string]string{
				"sidecar.istio.io/statsCompression": "brotli",
			},
		},
		{
			base: "stats_compression_zstd",
			annotations: map[string]string{
				"sidecar.istio.io/statsCompression": "zstd",
			},
		},
		{
			base: "stats_compression_unknown",
			annotations: map[string]string{
				"sidecar.istio.io/statsCompression": "unknown",
			},
		},
		{
			base: "stats_flush_interval",
			annotations: map[string]string{
				"sidecar.istio.io/statsFlushInterval": "10s",
			},
		},
		{
			base: "stats_eviction_interval",
			annotations: map[string]string{
				"sidecar.istio.io/statsEvictionInterval": "10s",
			},
		},
		{
			base: "invalid_stats_eviction_interval",
			annotations: map[string]string{
				"sidecar.istio.io/statsEvictionInterval": "11s",
			},
		},
		{
			base: "stats_interval",
			annotations: map[string]string{
				"sidecar.istio.io/statsFlushInterval":    "10s",
				"sidecar.istio.io/statsEvictionInterval": "20s",
			},
		},
	}

	test.SetForTest(t, &version.Info.Version, "binary-1.0")

	for _, c := range cases {
		t.Run("Bootstrap-"+c.base, func(t *testing.T) {
			out := t.TempDir()
			if c.setup != nil {
				c.setup()
			}
			if c.teardown != nil {
				defer c.teardown()
			}

			proxyConfig, err := loadProxyConfig(c.base, out, t)
			if err != nil {
				t.Fatalf("unable to load proxy config: %s\n%v", c.base, err)
			}

			_, localEnv := createEnv(t, map[string]string{}, c.annotations)
			for k, v := range c.envVars {
				localEnv = append(localEnv, k+"="+v)
			}

			plat := &fakePlatform{
				meta: c.platformMeta,
			}

			annoFile, err := os.CreateTemp("", "annotations")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(annoFile.Name())
			for k, v := range c.annotations {
				annoFile.WriteString(fmt.Sprintf("%s=%q\n", k, v))
			}

			node, err := GetNodeMetaData(MetadataOptions{
				ID:          "sidecar~1.2.3.4~foo~bar",
				Envs:        localEnv,
				Platform:    plat,
				InstanceIPs: []string{"10.3.3.3", "10.4.4.4", "10.5.5.5", "10.6.6.6", "10.4.4.4"},
				StsPort:     c.stsPort,
				ProxyConfig: proxyConfig,
				PilotSubjectAltName: []string{
					"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account",
				},
				OutlierLogPath:             "/dev/stdout",
				annotationFilePath:         annoFile.Name(),
				EnvoyPrometheusPort:        15090,
				EnvoyStatusPort:            15021,
				WorkloadIdentitySocketFile: "test.sock",
			})
			if err != nil {
				t.Fatal(err)
			}
			fn, err := New(Config{
				Node:             node,
				CompliancePolicy: c.compliancePolicy,
			}).CreateFile()
			if err != nil {
				t.Fatal(err)
			}

			read, err := os.ReadFile(fn)
			if err != nil {
				t.Error("Error reading generated file ", err)
				return
			}

			// apply minor modifications for the generated file so that tests are consistent
			// across different env setups
			err = os.WriteFile(fn, correctForEnvDifference(read, !c.checkLocality, out), 0o700)
			if err != nil {
				t.Error("Error modifying generated file ", err)
				return
			}

			// re-read generated file with the changes having been made
			read, err = os.ReadFile(fn)
			if err != nil {
				t.Error("Error reading generated file ", err)
				return
			}

			goldenFile := "testdata/" + c.base + "_golden.json"
			util.RefreshGoldenFile(t, read, goldenFile)

			golden, err := os.ReadFile(goldenFile)
			if err != nil {
				golden = []byte{}
			}

			realM := &bootstrap.Bootstrap{}
			goldenM := &bootstrap.Bootstrap{}

			jgolden, err := yaml.YAMLToJSON(golden)
			if err != nil {
				t.Fatalf("unable to convert: %s %v", c.base, err)
			}

			if err = protomarshal.Unmarshal(jgolden, goldenM); err != nil {
				t.Fatalf("invalid json %s %s\n%v", c.base, err, string(jgolden))
			}

			if err = goldenM.Validate(); err != nil {
				t.Fatalf("invalid golden %s: %v", c.base, err)
			}

			if err = protomarshal.Unmarshal(read, realM); err != nil {
				t.Fatalf("invalid json %v\n%s", err, string(read))
			}

			if err = realM.Validate(); err != nil {
				t.Fatalf("invalid generated file %s: %v", c.base, err)
			}

			checkStatsMatcher(t, realM, goldenM, c.stats, node.Metadata)
			checkStatsTags(t, goldenM)

			if c.check != nil {
				c.check(realM, t)
			}

			checkOpencensusConfig(t, realM, goldenM)

			if diff := cmp.Diff(goldenM, realM, protocmp.Transform()); diff != "" {
				t.Logf("difference: %s", diff)
				t.Fatalf("\n got: %s\nwant: %s", prettyPrint(read), prettyPrint(jgolden))
			}

			// Check if the LightStep access token file exists
			_, err = os.Stat(lightstepAccessTokenFile(path.Dir(fn)))
			if c.expectLightstepAccessToken {
				if os.IsNotExist(err) {
					t.Error("expected to find a LightStep access token file but none found")
				} else if err != nil {
					t.Error("error running Stat on file: ", err)
				}
			} else {
				if err == nil {
					t.Error("found a LightStep access token file but none was expected")
				} else if !os.IsNotExist(err) {
					t.Error("error running Stat on file: ", err)
				}
			}
		})
	}
}

func prettyPrint(b []byte) []byte {
	var out bytes.Buffer
	_ = json.Indent(&out, b, "", "  ")
	return out.Bytes()
}

func checkListStringMatcher(t *testing.T, got *matcher.ListStringMatcher, want string, typ string) {
	var patterns []string
	for _, pattern := range got.GetPatterns() {
		var pat string
		switch typ {
		case "prefix":
			pat = pattern.GetPrefix()
		case "suffix":
			pat = pattern.GetSuffix()
		case "regexp":
			// Migration tracked in https://github.com/istio/istio/issues/17127
			//nolint: staticcheck
			pat = pattern.GetSafeRegex().GetRegex()
		}

		if pat != "" {
			patterns = append(patterns, pat)
		}
	}
	gotPattern := strings.Join(patterns, ",")
	if want != gotPattern {
		t.Fatalf("%s mismatch:\ngot: %s\nwant: %s", typ, gotPattern, want)
	}
}

// nolint: staticcheck
func checkOpencensusConfig(t *testing.T, got, want *bootstrap.Bootstrap) {
	if want.Tracing == nil {
		return
	}

	if want.Tracing.Http.Name != "envoy.tracers.opencensus" {
		return
	}

	if diff := cmp.Diff(got.Tracing.Http, want.Tracing.Http, protocmp.Transform()); diff != "" {
		t.Fatalf("t diff: %v\ngot:\n %v\nwant:\n %v\n", diff, got.Tracing.Http, want.Tracing.Http)
	}
}

// Test that our golden files all have proper stat tags
func checkStatsTags(t *testing.T, got *bootstrap.Bootstrap) {
	for _, tag := range got.StatsConfig.StatsTags {
		// TODO: Add checks for other tags
		switch tag.TagName {
		case "cluster_name":
			checkClusterNameTag(t, tag.GetRegex())
		case "http_conn_manager_prefix":
			checkHTTPConnManagerPrefixTag(t, tag.GetRegex())
		}
	}
}

// Envoy will remove the first capture group and set the tag to the second capture group.
// We double check that all of the regexes we set return what we want here.

func checkHTTPConnManagerPrefixTag(t *testing.T, regex string) {
	if regex == "" {
		t.Fatal("cluster_name tag regex is empty")
	}

	compiledRegex, err := regexp.Compile(regex)
	if err != nil {
		t.Fatalf("invalid regex for cluster_name tag: %v", err)
	}

	tc := []struct {
		name               string
		statPrefix         string
		firstCaptureGroup  string
		secondCaptureGroup string
	}{
		{
			name:               "http_conn_manager_prefix stats tag - ipv4 address - single-segment",
			statPrefix:         "http.0.0.0.0;.downstream_rq_total",
			firstCaptureGroup:  "0.0.0.0;.",
			secondCaptureGroup: "0.0.0.0",
		},
		{
			name:               "http_conn_manager_prefix stats tag - ipv4 address - multi-segment",
			statPrefix:         "http.0.0.0.0;.jwt_authn.allowed",
			firstCaptureGroup:  "0.0.0.0;.",
			secondCaptureGroup: "0.0.0.0",
		},
		{
			name:               "http_conn_manager_prefix stats tag - ipv6 address - single-segment",
			statPrefix:         "http.2001:0000:130F:0000:0000:09C0:876A:130B;.downstream_rq_total",
			firstCaptureGroup:  "2001:0000:130F:0000:0000:09C0:876A:130B;.",
			secondCaptureGroup: "2001:0000:130F:0000:0000:09C0:876A:130B",
		},
		{
			name:               "http_conn_manager_prefix stats tag - ipv6 address - multi-segment",
			statPrefix:         "http.2001:0000:130F:0000:0000:09C0:876A:130B;.jwt_authn.allowed",
			firstCaptureGroup:  "2001:0000:130F:0000:0000:09C0:876A:130B;.",
			secondCaptureGroup: "2001:0000:130F:0000:0000:09C0:876A:130B",
		},
		{
			name:               "http_conn_manager_prefix stats tag - direction-prefixed ipv4 - single-segment",
			statPrefix:         "http.inbound_0.0.0.0;.downstream_rq_total",
			firstCaptureGroup:  "inbound_0.0.0.0;.",
			secondCaptureGroup: "inbound_0.0.0.0",
		},
		{
			name:               "http_conn_manager_prefix stats tag - direction-prefixed ipv4 - multi-segment",
			statPrefix:         "http.inbound_0.0.0.0;.jwt_authn.allowed",
			firstCaptureGroup:  "inbound_0.0.0.0;.",
			secondCaptureGroup: "inbound_0.0.0.0",
		},
		{
			name:               "http_conn_manager_prefix stats tag - direction-prefixed ipv6 - single-segment",
			statPrefix:         "http.inbound_2001:0000:130F:0000:0000:09C0:876A:130B;.downstream_rq_total",
			firstCaptureGroup:  "inbound_2001:0000:130F:0000:0000:09C0:876A:130B;.",
			secondCaptureGroup: "inbound_2001:0000:130F:0000:0000:09C0:876A:130B",
		},
		{
			name:               "http_conn_manager_prefix stats tag - direction-prefixed ipv6 - multi-segment",
			statPrefix:         "http.inbound_2001:0000:130F:0000:0000:09C0:876A:130B;.jwt_authn.allowed",
			firstCaptureGroup:  "inbound_2001:0000:130F:0000:0000:09C0:876A:130B;.",
			secondCaptureGroup: "inbound_2001:0000:130F:0000:0000:09C0:876A:130B",
		},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			subMatches := compiledRegex.FindStringSubmatch(tt.statPrefix)
			if subMatches == nil {
				t.Fatalf("cluster_name tag regex does not match cluster name %s", tt.statPrefix)
			}

			// The first index is the whole match followed by N number of capture groups.
			// There are 2 capture groups we expect in the regex, so we always check for 3
			if len(subMatches) != 3 {
				t.Fatalf("unexpected number of capture groups: %d. Submatches: %v", len(subMatches), subMatches)
			}
			// Now we examine both of the capture groups (which start at index 1)
			if subMatches[1] != tt.firstCaptureGroup {
				t.Fatalf("first capture group does not match %s, got %s", tt.firstCaptureGroup, subMatches[1])
			}

			// Finally, check the second capture group
			if subMatches[2] != tt.secondCaptureGroup {
				t.Fatalf("second capture group does not match %s, got %s", tt.secondCaptureGroup, subMatches[2])
			}
		})
	}
}

func checkClusterNameTag(t *testing.T, regex string) {
	if regex == "" {
		t.Fatal("cluster_name tag regex is empty")
	}

	compiledRegex, err := regexp.Compile(regex)
	if err != nil {
		t.Fatalf("invalid regex for cluster_name tag: %v", err)
	}

	tc := []struct {
		name               string
		clusterName        string
		firstCaptureGroup  string
		secondCaptureGroup string
	}{
		{
			name:               "cluster_name stats tag - external service",
			clusterName:        "cluster.outbound|443||api.facebook.com;.upstream_rq_retry",
			firstCaptureGroup:  ".outbound|443||api.facebook.com;",
			secondCaptureGroup: "outbound|443||api.facebook.com",
		},
		{
			name:               "cluster_name stats tag - internal kubernetes service",
			clusterName:        "cluster.outbound|443||kubernetes.default.svc.cluster.local;.upstream_rq_retry",
			firstCaptureGroup:  ".outbound|443||kubernetes.default.svc.cluster.local;",
			secondCaptureGroup: "outbound|443||kubernetes.default.svc.cluster.local",
		},
		{
			name:               "cluster_name stats tag - no dots in cluster name",
			clusterName:        "cluster.xds-grpc;.upstream_rq_retry",
			firstCaptureGroup:  ".xds-grpc;",
			secondCaptureGroup: "xds-grpc",
		},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			subMatches := compiledRegex.FindStringSubmatch(tt.clusterName)
			if subMatches == nil {
				t.Fatalf("cluster_name tag regex does not match cluster name %s", tt.clusterName)
			}

			// The first index is the whole match followed by N number of capture groups.
			// There are 2 capture groups we expect in the regex, so we always check for 3
			if len(subMatches) != 3 {
				t.Fatalf("unexpected number of capture groups: %d. Submatches: %v", len(subMatches), subMatches)
			}
			// Now we examine both of the capture groups (which start at index 1)
			if subMatches[1] != tt.firstCaptureGroup {
				t.Fatalf("first capture group does not match %s, got %s", tt.firstCaptureGroup, subMatches[1])
			}

			// Finally, check the second capture group
			if subMatches[2] != tt.secondCaptureGroup {
				t.Fatalf("second capture group does not match %s, got %s", tt.secondCaptureGroup, subMatches[2])
			}
		})
	}
}

func checkStatsMatcher(t *testing.T, got, want *bootstrap.Bootstrap, stats stats, meta *model.BootstrapNodeMetadata) {
	gsm := got.GetStatsConfig().GetStatsMatcher()
	variablePrefixes := ""
	if meta.EnableHBONE {
		variablePrefixes = "workload_discovery,"
	}
	if stats.prefixes == "" {
		stats.prefixes = v2Prefixes + variablePrefixes + requiredEnvoyStatsMatcherInclusionPrefixes + v2Suffix
	} else {
		stats.prefixes = v2Prefixes + stats.prefixes + "," + requiredEnvoyStatsMatcherInclusionPrefixes + v2Suffix
	}
	if stats.suffixes == "" {
		stats.suffixes = rbacEnvoyStatsMatcherInclusionSuffix
	} else {
		stats.suffixes += "," + rbacEnvoyStatsMatcherInclusionSuffix
	}

	if stats.regexps == "" {
		stats.regexps = requiredEnvoyStatsMatcherInclusionRegexes
	} else {
		stats.regexps += "," + requiredEnvoyStatsMatcherInclusionRegexes
	}

	if err := gsm.Validate(); err != nil {
		t.Fatalf("Generated invalid matcher: %v", err)
	}

	checkListStringMatcher(t, gsm.GetInclusionList(), stats.prefixes, "prefix")
	checkListStringMatcher(t, gsm.GetInclusionList(), stats.suffixes, "suffix")
	checkListStringMatcher(t, gsm.GetInclusionList(), stats.regexps, "regexp")

	// remove StatsMatcher for general matching
	got.StatsConfig.StatsMatcher = nil
	want.StatsConfig.StatsMatcher = nil

	// remove StatsMatcher metadata from matching
	delete(got.Node.Metadata.Fields, annotation.SidecarStatsInclusionPrefixes.Name)
	delete(want.Node.Metadata.Fields, annotation.SidecarStatsInclusionPrefixes.Name)
	delete(got.Node.Metadata.Fields, annotation.SidecarStatsInclusionSuffixes.Name)
	delete(want.Node.Metadata.Fields, annotation.SidecarStatsInclusionSuffixes.Name)
	delete(got.Node.Metadata.Fields, annotation.SidecarStatsInclusionRegexps.Name)
	delete(want.Node.Metadata.Fields, annotation.SidecarStatsInclusionRegexps.Name)
}

type regexReplacement struct {
	pattern     *regexp.Regexp
	replacement []byte
}

// correctForEnvDifference corrects the portions of a generated bootstrap config that vary depending on the environment
// so that they match the golden file's expected value.
func correctForEnvDifference(in []byte, excludeLocality bool, tmpDir string) []byte {
	replacements := []regexReplacement{
		// Lightstep access tokens are written to a file and that path is dependent upon the environment variables that
		// are set. Standardize the path so that golden files can be properly checked.
		{
			pattern:     regexp.MustCompile(`("access_token_file": ").*(lightstep_access_token.txt")`),
			replacement: []byte("$1/test-path/$2"),
		},
		{
			// Example: "customConfigFile":"../../tools/packaging/common/envoy_bootstrap.json"
			// The path may change in CI/other machines
			pattern:     regexp.MustCompile(`("customConfigFile":").*(envoy_bootstrap.json")`),
			replacement: []byte(`"customConfigFile":"envoy_bootstrap.json"`),
		},
		{
			pattern:     regexp.MustCompile(tmpDir),
			replacement: []byte(`/tmp`),
		},
		{
			pattern:     regexp.MustCompile(`("path": ".*/XDS")`),
			replacement: []byte(`"path": "/tmp/XDS"`),
		},
	}
	if excludeLocality {
		// zone and region can vary based on the environment, so it shouldn't be considered in the diff.
		replacements = append(replacements,
			regexReplacement{
				pattern:     regexp.MustCompile(`"zone": ".+"`),
				replacement: []byte("\"zone\": \"\""),
			},
			regexReplacement{
				pattern:     regexp.MustCompile(`"region": ".+"`),
				replacement: []byte("\"region\": \"\""),
			})
	}

	out := in
	for _, r := range replacements {
		out = r.pattern.ReplaceAll(out, r.replacement)
	}
	return out
}

func loadProxyConfig(base, out string, _ *testing.T) (*meshconfig.ProxyConfig, error) {
	content, err := os.ReadFile("testdata/" + base + ".proxycfg")
	if err != nil {
		return nil, err
	}
	cfg := &meshconfig.ProxyConfig{}

	err = prototext.Unmarshal(content, cfg)
	if err != nil {
		return nil, err
	}

	// Exported from makefile or env
	cfg.ConfigPath = filepath.Join(out, "/bootstrap/", base)
	cfg.CustomConfigFile = filepath.Join(env.IstioSrc, "/tools/packaging/common/envoy_bootstrap.json")
	if cfg.StatusPort == 0 {
		cfg.StatusPort = 15020
	}
	return cfg, nil
}

// createEnv takes labels and annotations are returns environment in go format.
func createEnv(t *testing.T, labels map[string]string, anno map[string]string) (map[string]string, []string) {
	merged := map[string]string{}
	mergeMap(merged, labels)
	mergeMap(merged, anno)

	envs := make([]string, 0)

	if labels != nil {
		envs = append(envs, encodeAsJSON(t, labels, "LABELS"))
	}

	if anno != nil {
		envs = append(envs, encodeAsJSON(t, anno, "ANNOTATIONS"))
	}
	return merged, envs
}

func encodeAsJSON(t *testing.T, data map[string]string, name string) string {
	jsonStr, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal %s %v: %v", name, data, err)
	}
	return IstioMetaJSONPrefix + name + "=" + string(jsonStr)
}

func TestNodeMetadataEncodeEnvWithIstioMetaPrefix(t *testing.T) {
	originalKey := "foo"
	notIstioMetaKey := "NOT_AN_" + IstioMetaPrefix + originalKey
	anIstioMetaKey := IstioMetaPrefix + originalKey
	envs := []string{
		notIstioMetaKey + "=bar",
		anIstioMetaKey + "=baz",
	}
	node, err := GetNodeMetaData(MetadataOptions{
		ID:          "test",
		Envs:        envs,
		ProxyConfig: &meshconfig.ProxyConfig{},
	})
	nm := node.Metadata
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := nm.Raw[notIstioMetaKey]; ok {
		t.Fatalf("%s should not be encoded in node metadata", notIstioMetaKey)
	}

	if _, ok := nm.Raw[anIstioMetaKey]; ok {
		t.Fatalf("%s should not be encoded in node metadata. The prefix '%s' should be stripped", anIstioMetaKey, IstioMetaPrefix)
	}
	if val, ok := nm.Raw[originalKey]; !ok {
		t.Fatalf("%s has the prefix %s and it should be encoded in the node metadata", originalKey, IstioMetaPrefix)
	} else if val != "baz" {
		t.Fatalf("unexpected value node metadata %s. got %s, want: %s", originalKey, val, "baz")
	}
}

func TestNodeMetadata(t *testing.T) {
	envs := []string{
		"ISTIO_META_ISTIO_VERSION=1.0.0",
		`ISTIO_METAJSON_LABELS={"foo":"bar"}`,
	}
	node, err := GetNodeMetaData(MetadataOptions{
		ID:          "test",
		Envs:        envs,
		ProxyConfig: &meshconfig.ProxyConfig{},
	})
	nm := node.Metadata
	if err != nil {
		t.Fatal(err)
	}
	if nm.IstioVersion != "1.0.0" {
		t.Fatalf("Expected IstioVersion 1.0.0, got %v", nm.IstioVersion)
	}
	if !reflect.DeepEqual(nm.Labels, map[string]string{"foo": "bar"}) {
		t.Fatalf("Expected Labels foo: bar, got %v", nm.Labels)
	}
}

func mergeMap(to map[string]string, from map[string]string) {
	for k, v := range from {
		to[k] = v
	}
}

type fakePlatform struct {
	platform.Environment

	meta   map[string]string
	labels map[string]string
}

func (f *fakePlatform) Metadata() map[string]string {
	return f.meta
}

func (f *fakePlatform) Locality() *core.Locality {
	return &core.Locality{}
}

func (f *fakePlatform) Labels() map[string]string {
	return f.labels
}

func (f *fakePlatform) IsKubernetes() bool {
	return true
}
