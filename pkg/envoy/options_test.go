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

package envoy_test

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/envoy"
)

var testConfigPath = absPath("testdata/bootstrap.json")

func TestNewOptions(t *testing.T) {
	g := NewWithT(t)

	args := []string{"--config-path", testConfigPath, "--k2", "v2", "--k3"}
	actuals, err := envoy.NewOptions(args...)
	g.Expect(err).To(BeNil())
	g.Expect(actuals.Validate()).To(BeNil())

	g.Expect(actuals.ToArgs()).To(Equal(args))
}

func TestNewOptionsWithoutConfigPathShouldFail(t *testing.T) {
	g := NewWithT(t)

	args := []string{"--k2", "v2", "--k3"}
	actuals, err := envoy.NewOptions(args...)
	g.Expect(err).To(BeNil())
	g.Expect(actuals.Validate()).ToNot(BeNil())
}

func TestNewOptionsWithInvalidFlagShouldFail(t *testing.T) {
	g := NewWithT(t)

	args := []string{"--config-path", testConfigPath, "--k1", "v1", "k2", "v2"}
	_, err := envoy.NewOptions(args...)
	g.Expect(err).ToNot(BeNil())
}

func TestNewOptionsWithDuplicateFlagShouldFail(t *testing.T) {
	g := NewWithT(t)

	args := []string{"--config-path", testConfigPath, "--k1", "v1", "--k1", "v2"}
	actuals, err := envoy.NewOptions(args...)
	g.Expect(err).To(BeNil())
	g.Expect(actuals.Validate()).ToNot(BeNil())
}

func TestDuplicateOptions(t *testing.T) {
	g := NewWithT(t)

	options := envoy.Options{envoy.ConfigYaml("{a:b}"), envoy.ConfigYaml("{a:b}")}
	g.Expect(options.Validate()).ToNot(BeNil())
}

func TestGoodOptions(t *testing.T) {
	cases := []struct {
		name         string
		expectedArgs []string
		options      envoy.Options
	}{
		{
			name:         "config-yaml",
			expectedArgs: []string{"--config-yaml", "{a:b}"},
			options:      envoy.Options{envoy.ConfigYaml("{a:b}")},
		},
		{
			name:         "log-level",
			expectedArgs: []string{"--config-yaml", "{}", "--log-level", "info"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.LogLevelInfo},
		},
		{
			name:         "component-log-level",
			expectedArgs: []string{"--config-yaml", "{}", "--component-log-level", "a:info,b:warning"},
			options: envoy.Options{envoy.ConfigYaml("{}"), envoy.ComponentLogLevels{
				envoy.ComponentLogLevel{
					Name:  "a",
					Level: envoy.LogLevelInfo,
				},
				envoy.ComponentLogLevel{
					Name:  "b",
					Level: envoy.LogLevelWarning,
				},
			}},
		},
		{
			name:         "local-address-ip-version",
			expectedArgs: []string{"--config-yaml", "{}", "--local-address-ip-version", "v4"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.LocalAddressIPVersion(envoy.IPV4)},
		},
		{
			name:         "base-id",
			expectedArgs: []string{"--config-yaml", "{}", "--base-id", "123"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.BaseID(123)},
		},
		{
			name:         "concurrency",
			expectedArgs: []string{"--config-yaml", "{}", "--concurrency", "4"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.Concurrency(4)},
		},
		{
			name:         "disable-hot-restart:true",
			expectedArgs: []string{"--config-yaml", "{}", "--disable-hot-restart"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.DisableHotRestart(true)},
		},
		{
			name:         "disable-hot-restart:false",
			expectedArgs: []string{"--config-yaml", "{}"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.DisableHotRestart(false)},
		},
		{
			name:         "log-path",
			expectedArgs: []string{"--config-yaml", "{}", "--log-path", "fake/path"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.LogPath("fake/path")},
		},
		{
			name:         "log-format",
			expectedArgs: []string{"--config-yaml", "{}", "--log-format", "some format"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.LogFormat("some format")},
		},
		{
			name:         "service-cluster",
			expectedArgs: []string{"--config-yaml", "{}", "--service-cluster", "fake-cluster"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.ServiceCluster("fake-cluster")},
		},
		{
			name:         "service-node",
			expectedArgs: []string{"--config-yaml", "{}", "--service-node", "fake-node"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.ServiceNode("fake-node")},
		},
		{
			name:         "drain-time-s",
			expectedArgs: []string{"--config-yaml", "{}", "--drain-time-s", "15"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.DrainDuration(15 * time.Second)},
		},
		{
			name:         "parent-shutdown-time-s",
			expectedArgs: []string{"--config-yaml", "{}", "--parent-shutdown-time-s", "15"},
			options:      envoy.Options{envoy.ConfigYaml("{}"), envoy.ParentShutdownDuration(15 * time.Second)},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(c.options.Validate()).To(BeNil())
			g.Expect(c.options.ToArgs()).To(Equal(c.expectedArgs))
		})
	}
}

func TestInvalidLogLevel(t *testing.T) {
	g := NewWithT(t)
	options := envoy.Options{envoy.ConfigYaml("{}"), envoy.LogLevel("bad")}
	g.Expect(options.Validate()).ToNot(BeNil())
}

func TestInvalidComponentLogLevels(t *testing.T) {
	g := NewWithT(t)
	options := envoy.Options{envoy.ConfigYaml("{}"), envoy.ComponentLogLevels{
		envoy.ComponentLogLevel{
			Name:  "a",
			Level: envoy.LogLevel("bad"),
		},
	}}
	g.Expect(options.Validate()).ToNot(BeNil())
}

func TestInvalidLocalAddressIPVersion(t *testing.T) {
	g := NewWithT(t)
	options := envoy.Options{envoy.ConfigYaml("{}"), envoy.LocalAddressIPVersion("bad")}
	g.Expect(options.Validate()).ToNot(BeNil())
}

func TestInvalidConfigPath(t *testing.T) {
	g := NewWithT(t)
	options := envoy.Options{envoy.ConfigPath("bad/file/path")}
	g.Expect(options.Validate()).ToNot(BeNil())
}

func TestInvalidBaseID(t *testing.T) {
	g := NewWithT(t)
	actuals, err := envoy.NewOptions("--config-path", testConfigPath, "--base-id", "bad-int-value")
	g.Expect(err).To(BeNil())
	g.Expect(actuals.Validate()).ToNot(BeNil())
}

func TestInvalidConcurrency(t *testing.T) {
	g := NewWithT(t)
	actuals, err := envoy.NewOptions("--config-path", testConfigPath, "--concurrency", "bad-int-value")
	g.Expect(err).To(BeNil())
	g.Expect(actuals.Validate()).ToNot(BeNil())
}

func TestInvalidDrainDuration(t *testing.T) {
	g := NewWithT(t)
	actuals, err := envoy.NewOptions("--config-path", testConfigPath, "--drain-time-s", "bad-int-value")
	g.Expect(err).To(BeNil())
	g.Expect(actuals.Validate()).ToNot(BeNil())
}

func TestInvalidParentShutdownDuration(t *testing.T) {
	g := NewWithT(t)
	actuals, err := envoy.NewOptions("--config-path", testConfigPath, "--parent-shutdown-time-s", "bad-int-value")
	g.Expect(err).To(BeNil())
	g.Expect(actuals.Validate()).ToNot(BeNil())
}

func TestGenerateBaseID(t *testing.T) {
	g := NewWithT(t)

	baseID := envoy.GenerateBaseID()
	expected := []string{"--config-yaml", "{}", "--base-id", baseID.FlagValue()}
	options := envoy.Options{envoy.ConfigYaml("{}"), baseID}
	g.Expect(options.Validate()).To(BeNil())
	g.Expect(options.ToArgs()).To(Equal(expected))
}

func TestParseComponentLogLevels(t *testing.T) {
	g := NewWithT(t)
	expected := envoy.ComponentLogLevels{
		envoy.ComponentLogLevel{
			Name:  "a",
			Level: envoy.LogLevelInfo,
		},
		envoy.ComponentLogLevel{
			Name:  "b",
			Level: envoy.LogLevelWarning,
		},
	}
	actual := envoy.ParseComponentLogLevels("a:info,b:warning")
	g.Expect(actual).To(Equal(expected))
}
