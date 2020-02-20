// Copyright 2018 Istio Authors
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
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"regexp"
	"strings"
	"testing"

	v1 "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v2 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v2"
	tracev2 "github.com/envoyproxy/go-control-plane/envoy/config/trace/v2"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/jsonpb"
	diff "gopkg.in/d4l3k/messagediff.v1"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/test/env"
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
// If the template is updated, copy the new golden files from out:
// cp $TOP/out/linux_amd64/release/bootstrap/all/envoy-rev0.json pkg/bootstrap/testdata/all_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/auth/envoy-rev0.json pkg/bootstrap/testdata/auth_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/default/envoy-rev0.json pkg/bootstrap/testdata/default_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/tracing_datadog/envoy-rev0.json pkg/bootstrap/testdata/tracing_datadog_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/tracing_lightstep/envoy-rev0.json pkg/bootstrap/testdata/tracing_lightstep_golden.json
// cp $TOP/out/linux_amd64/release/bootstrap/tracing_zipkin/envoy-rev0.json pkg/bootstrap/testdata/tracing_zipkin_golden.json
func TestGolden(t *testing.T) {
	out := env.ISTIO_OUT.Value() // defined in the makefile
	if out == "" {
		out = "/tmp"
	}

	cases := []struct {
		base                       string
		envVars                    map[string]string
		annotations                map[string]string
		sdsUDSPath                 string
		sdsTokenPath               string
		expectLightstepAccessToken bool
		stats                      stats
		checkLocality              bool
		setup                      func()
		teardown                   func()
		check                      func(got *v2.Bootstrap, t *testing.T)
	}{
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
			base: "tracing_stackdriver",
			setup: func() {
				credPath := out + "/sd_cred.json"
				if err := ioutil.WriteFile(credPath, []byte(`{"type": "service_account", "project_id": "my-sd-project"}`), os.ModePerm); err != nil {
					t.Fatalf("unable write file: %v", err)
				}
				_ = os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credPath)
			},
			teardown: func() {
				credPath := out + "/sd_cred.json"
				_ = os.Unsetenv("GOOGLE_APPLICATION_CREDENTIALS")
				_ = os.Remove(credPath)
			},
			check: func(got *v2.Bootstrap, t *testing.T) {
				// nolint: staticcheck
				cfg := got.Tracing.Http.GetConfig()
				sdMsg := tracev2.OpenCensusConfig{}
				if err := conversion.StructToMessage(cfg, &sdMsg); err != nil {
					t.Fatalf("unable to parse: %v %v", cfg, err)
				}

				want := tracev2.OpenCensusConfig{
					TraceConfig: &v1.TraceConfig{
						Sampler: &v1.TraceConfig_ConstantSampler{
							ConstantSampler: &v1.ConstantSampler{
								Decision: v1.ConstantSampler_ALWAYS_PARENT,
							},
						},
						MaxNumberOfAttributes:    200,
						MaxNumberOfAnnotations:   201,
						MaxNumberOfMessageEvents: 201,
						MaxNumberOfLinks:         200,
					},
					StackdriverExporterEnabled: true,
					StdoutExporterEnabled:      true,
					StackdriverProjectId:       "my-sd-project",
					IncomingTraceContext: []tracev2.OpenCensusConfig_TraceContext{
						tracev2.OpenCensusConfig_CLOUD_TRACE_CONTEXT,
						tracev2.OpenCensusConfig_TRACE_CONTEXT,
						tracev2.OpenCensusConfig_GRPC_TRACE_BIN,
						tracev2.OpenCensusConfig_B3},
					OutgoingTraceContext: []tracev2.OpenCensusConfig_TraceContext{
						tracev2.OpenCensusConfig_CLOUD_TRACE_CONTEXT,
						tracev2.OpenCensusConfig_TRACE_CONTEXT,
						tracev2.OpenCensusConfig_GRPC_TRACE_BIN,
						tracev2.OpenCensusConfig_B3},
				}

				p, equal := diff.PrettyDiff(sdMsg, want)
				if !equal {
					t.Fatalf("t diff: %v\ngot: %v\nwant: %v\n", p, sdMsg, want)
				}
			},
		},
		{
			// Specify zipkin/statsd address, similar with the default config in v1 tests
			base: "all",
		},
		{
			base: "stats_inclusion",
			annotations: map[string]string{
				"sidecar.istio.io/statsInclusionPrefixes": "prefix1,prefix2",
				"sidecar.istio.io/statsInclusionSuffixes": "suffix1,suffix2",
				"sidecar.istio.io/extraStatTags":          "dlp_status,dlp_error",
			},
			stats: stats{prefixes: "prefix1,prefix2",
				suffixes: "suffix1,suffix2"},
		},
		{
			base: "stats_inclusion",
			annotations: map[string]string{
				"sidecar.istio.io/statsInclusionSuffixes": upstreamStatsSuffixes + "," + downstreamStatsSuffixes,
				"sidecar.istio.io/extraStatTags":          "dlp_status,dlp_error",
			},
			stats: stats{
				suffixes: upstreamStatsSuffixes + "," + downstreamStatsSuffixes},
		},
		{
			base: "stats_inclusion",
			annotations: map[string]string{
				"sidecar.istio.io/statsInclusionPrefixes": "http.{pod_ip}_",
				"sidecar.istio.io/extraStatTags":          "dlp_status,dlp_error",
			},
			// {pod_ip} is unrolled
			stats: stats{prefixes: "http.10.3.3.3_,http.10.4.4.4_,http.10.5.5.5_,http.10.6.6.6_"},
		},
		{
			base: "stats_inclusion",
			annotations: map[string]string{
				"sidecar.istio.io/statsInclusionRegexps": "http.[0-9]*\\.[0-9]*\\.[0-9]*\\.[0-9]*_8080.downstream_rq_time",
				"sidecar.istio.io/extraStatTags":         "dlp_status,dlp_error",
			},
			stats: stats{regexps: "http.[0-9]*\\.[0-9]*\\.[0-9]*\\.[0-9]*_8080.downstream_rq_time"},
		},
	}

	for _, c := range cases {
		t.Run("Bootstrap-"+c.base, func(t *testing.T) {
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

			fn, err := New(Config{
				Node:           "sidecar~1.2.3.4~foo~bar",
				DNSRefreshRate: "60s",
				Proxy:          proxyConfig,
				PlatEnv:        &fakePlatform{},
				PilotSubjectAltName: []string{
					"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account"},
				LocalEnv:       localEnv,
				NodeIPs:        []string{"10.3.3.3", "10.4.4.4", "10.5.5.5", "10.6.6.6", "10.4.4.4"},
				SDSUDSPath:     c.sdsUDSPath,
				SDSTokenPath:   c.sdsTokenPath,
				OutlierLogPath: "/dev/stdout",
			}).CreateFileForEpoch(0)
			if err != nil {
				t.Fatal(err)
			}

			read, err := ioutil.ReadFile(fn)
			if err != nil {
				t.Error("Error reading generated file ", err)
				return
			}

			// apply minor modifications for the generated file so that tests are consistent
			// across different env setups
			err = ioutil.WriteFile(fn, correctForEnvDifference(read, !c.checkLocality), 0700)
			if err != nil {
				t.Error("Error modifying generated file ", err)
				return
			}

			// re-read generated file with the changes having been made
			read, err = ioutil.ReadFile(fn)
			if err != nil {
				t.Error("Error reading generated file ", err)
				return
			}

			golden, err := ioutil.ReadFile("testdata/" + c.base + "_golden.json")
			if err != nil {
				golden = []byte{}
			}

			realM := v2.Bootstrap{}
			goldenM := v2.Bootstrap{}

			jgolden, err := yaml.YAMLToJSON(golden)

			if err != nil {
				t.Fatalf("unable to convert: %s %v", c.base, err)
			}

			if err = jsonpb.UnmarshalString(string(jgolden), &goldenM); err != nil {
				t.Fatalf("invalid json %s %s\n%v", c.base, err, string(jgolden))
			}

			if err = goldenM.Validate(); err != nil {
				t.Fatalf("invalid golden %s: %v", c.base, err)
			}

			jreal, err := yaml.YAMLToJSON(read)

			if err != nil {
				t.Fatalf("unable to convert: %s (%s) %v", c.base, fn, err)
			}

			if err = jsonpb.UnmarshalString(string(jreal), &realM); err != nil {
				t.Fatalf("invalid json %v\n%s", err, string(read))
			}

			if err = realM.Validate(); err != nil {
				t.Fatalf("invalid generated file %s: %v", c.base, err)
			}

			checkStatsMatcher(t, &realM, &goldenM, c.stats)

			if c.check != nil {
				c.check(&realM, t)
			}

			checkOpencensusConfig(t, &realM, &goldenM)

			if !reflect.DeepEqual(realM, goldenM) {
				s, _ := diff.PrettyDiff(goldenM, realM)
				t.Logf("difference: %s", s)
				t.Fatalf("\n got: %v\nwant: %v", realM, goldenM)
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
			pat = pattern.GetRegex()
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

func checkOpencensusConfig(t *testing.T, got, want *v2.Bootstrap) {
	if want.Tracing == nil {
		return
	}

	if want.Tracing.Http.Name != "envoy.tracers.opencensus" {
		return
	}

	if !reflect.DeepEqual(got.Tracing.Http, want.Tracing.Http) {
		p, _ := diff.PrettyDiff(got.Tracing.Http, want.Tracing.Http)
		t.Fatalf("t diff: %v\ngot:\n %v\nwant:\n %v\n", p, got.Tracing.Http, want.Tracing.Http)
	}
}

func checkStatsMatcher(t *testing.T, got, want *v2.Bootstrap, stats stats) {
	gsm := got.GetStatsConfig().GetStatsMatcher()

	if stats.prefixes == "" {
		stats.prefixes = v2Prefixes + requiredEnvoyStatsMatcherInclusionPrefixes
	} else {
		stats.prefixes = v2Prefixes + stats.prefixes + "," + requiredEnvoyStatsMatcherInclusionPrefixes
	}

	if stats.suffixes == "" {
		stats.suffixes = requiredEnvoyStatsMatcherInclusionSuffix
	} else {
		stats.suffixes += "," + requiredEnvoyStatsMatcherInclusionSuffix
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
func correctForEnvDifference(in []byte, excludeLocality bool) []byte {
	replacements := []regexReplacement{
		// Lightstep access tokens are written to a file and that path is dependent upon the environment variables that
		// are set. Standardize the path so that golden files can be properly checked.
		{
			pattern:     regexp.MustCompile(`("access_token_file": ").*(lightstep_access_token.txt")`),
			replacement: []byte("$1/test-path/$2"),
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
	content, err := ioutil.ReadFile("testdata/" + base + ".proxycfg")
	if err != nil {
		return nil, err
	}
	cfg := &meshconfig.ProxyConfig{}
	err = proto.UnmarshalText(string(content), cfg)
	if err != nil {
		return nil, err
	}

	// Exported from makefile or env
	cfg.ConfigPath = out + "/bootstrap/" + base
	gobase := os.Getenv("ISTIO_GO")
	if gobase == "" {
		gobase = "../.."
	}
	cfg.CustomConfigFile = gobase + "/tools/packaging/common/envoy_bootstrap_v2.json"
	return cfg, nil
}

func TestIsIPv6Proxy(t *testing.T) {
	tests := []struct {
		name     string
		addrs    []string
		expected bool
	}{
		{
			name:     "ipv4 only",
			addrs:    []string{"1.1.1.1", "127.0.0.1", "2.2.2.2"},
			expected: false,
		},
		{
			name:     "ipv6 only",
			addrs:    []string{"1111:2222::1", "::1", "2222:3333::1"},
			expected: true,
		},
		{
			name:     "mixed ipv4 and ipv6",
			addrs:    []string{"1111:2222::1", "::1", "127.0.0.1", "2.2.2.2", "2222:3333::1"},
			expected: false,
		},
	}
	for _, tt := range tests {
		result := isIPv6Proxy(tt.addrs)
		if result != tt.expected {
			t.Errorf("Test %s failed, expected: %t got: %t", tt.name, tt.expected, result)
		}
	}
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
	nm, _, err := getNodeMetaData(envs, nil, nil, false, 0)
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
	nm, _, err := getNodeMetaData(envs, nil, nil, false, 0)
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

	meta map[string]string
}

func (f *fakePlatform) Metadata() map[string]string {
	return f.meta
}

func (f *fakePlatform) Locality() *core.Locality {
	return &core.Locality{}
}
