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

package configdump

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	adminv3 "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/util/configdump"
	sdscompare "istio.io/istio/istioctl/pkg/writer/compare/sds"
	"istio.io/istio/pkg/util/protomarshal"
)

// ConfigWriter is a writer for processing responses from the Envoy Admin config_dump endpoint
type ConfigWriter struct {
	Stdout     io.Writer
	configDump *configdump.Wrapper
}

// includeConfigType is a flag to indicate whether to include the config type in the output
var includeConfigType bool

func SetPrintConfigTypeInSummary(p bool) {
	includeConfigType = p
}

// Prime loads the config dump into the writer ready for printing
func (c *ConfigWriter) Prime(b []byte) error {
	w := &configdump.Wrapper{}
	err := w.UnmarshalJSON(b)
	if err != nil {
		return fmt.Errorf("error unmarshalling config dump response from Envoy: %v", err)
	}
	c.configDump = w
	return nil
}

// PrintBootstrapDump prints just the bootstrap config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintBootstrapDump(outputFormat string) error {
	if c.configDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}
	bootstrapDump, err := c.configDump.GetBootstrapConfigDump()
	if err != nil {
		return err
	}
	out, err := protomarshal.ToJSONWithIndent(bootstrapDump, "    ")
	if err != nil {
		return fmt.Errorf("unable to marshal bootstrap in Envoy config dump: %v", err)
	}
	if outputFormat == "yaml" {
		outbyte, err := yaml.JSONToYAML([]byte(out))
		if err != nil {
			return err
		}
		out = string(outbyte)
	}
	fmt.Fprintln(c.Stdout, out)
	return nil
}

// PrintSecretDump prints just the secret config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintSecretDump(outputFormat string) error {
	if c.configDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}
	secretDump, err := c.configDump.GetSecretConfigDump()
	if err != nil {
		return fmt.Errorf("sidecar doesn't support secrets: %v", err)
	}
	out, err := protomarshal.ToJSONWithIndent(secretDump, "    ")
	if err != nil {
		return fmt.Errorf("unable to marshal secrets in Envoy config dump: %v", err)
	}
	if outputFormat == "yaml" {
		outbyte, err := yaml.JSONToYAML([]byte(out))
		if err != nil {
			return err
		}
		out = string(outbyte)
	}
	fmt.Fprintln(c.Stdout, out)
	return nil
}

// PrintSecretSummary prints a summary of dynamic active secrets from the config dump
func (c *ConfigWriter) PrintSecretSummary() error {
	secretDump, err := c.configDump.GetSecretConfigDump()
	if err != nil {
		return err
	}
	if len(secretDump.DynamicActiveSecrets) == 0 &&
		len(secretDump.DynamicWarmingSecrets) == 0 {
		fmt.Fprintln(c.Stdout, "No active or warming secrets found.")
		return nil
	}
	secretItems, err := sdscompare.GetEnvoySecrets(c.configDump)
	if err != nil {
		return err
	}

	secretWriter := sdscompare.NewSDSWriter(c.Stdout, sdscompare.TABULAR)
	return secretWriter.PrintSecretItems(secretItems)
}

func (c *ConfigWriter) PrintFullSummary(cf ClusterFilter, lf ListenerFilter, rf RouteFilter, epf EndpointFilter) error {
	if err := c.PrintBootstrapSummary(); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintClusterSummary(cf); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintListenerSummary(lf); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintRouteSummary(rf); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintSecretSummary(); err != nil {
		return err
	}
	_, _ = c.Stdout.Write([]byte("\n"))
	if err := c.PrintEndpointsSummary(epf); err != nil {
		return err
	}
	return nil
}

// PrintBootstrapSummary prints bootstrap information for Istio and Envoy from the config dump
func (c *ConfigWriter) PrintBootstrapSummary() error {
	if c.configDump == nil {
		return fmt.Errorf("config writer has not been primed")
	}

	bootstrapDump, err := c.configDump.GetBootstrapConfigDump()
	if err != nil {
		return err
	}

	var (
		istioVersion, istioProxySha = c.getIstioVersionInfo(bootstrapDump)
		envoyVersion                = c.getUserAgentVersionInfo(bootstrapDump)

		tw = tabwriter.NewWriter(c.Stdout, 0, 8, 1, ' ', 0)
	)

	if len(istioVersion) > 0 {
		fmt.Fprintf(tw, "Istio Version:\t%s\n", istioVersion)
	}
	if len(istioProxySha) > 0 {
		fmt.Fprintf(tw, "Istio Proxy Version:\t%s\n", istioProxySha)
	}
	if len(envoyVersion) > 0 {
		fmt.Fprintf(tw, "Envoy Version:\t%s\n", envoyVersion)
	}

	return tw.Flush()
}

// PrintPodRootCAFromDynamicSecretDump prints just pod's root ca from dynamic secret config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintPodRootCAFromDynamicSecretDump() ([]byte, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	secretDump, err := c.configDump.GetSecretConfigDump()
	if err != nil {
		return nil, fmt.Errorf("sidecar doesn't support secrets: %v", err)
	}
	for _, secret := range secretDump.DynamicActiveSecrets {
		// check the ROOTCA from secret dump
		if secret.Name == "ROOTCA" {
			rootCAData, err := c.configDump.GetRootCAFromSecretConfigDump(secret.GetSecret())
			if err != nil {
				return nil, fmt.Errorf("can not dump ROOTCA from secret: %v", err)
			}
			return rootCAData, nil
		}
	}
	return nil, fmt.Errorf("cannot find ROOTCA from secret")
}

func (c *ConfigWriter) getIstioVersionInfo(bootstrapDump *adminv3.BootstrapConfigDump) (version, sha string) {
	const (
		istioVersionKey  = "ISTIO_VERSION"
		istioProxyShaKey = "ISTIO_PROXY_SHA"
	)

	md := bootstrapDump.GetBootstrap().GetNode().GetMetadata().GetFields()

	if versionPB, ok := md[istioVersionKey]; ok {
		version = versionPB.GetStringValue()
	}
	if shaPB, ok := md[istioProxyShaKey]; ok {
		sha = shaPB.GetStringValue()
		if shaParts := strings.Split(sha, ":"); len(shaParts) > 1 {
			sha = shaParts[1]
		}
	}

	return version, sha
}

func (c *ConfigWriter) getUserAgentVersionInfo(bootstrapDump *adminv3.BootstrapConfigDump) string {
	const (
		buildLabelKey = "build.label"
		buildTypeKey  = "build.type"
		statusKey     = "revision.status"
		sslVersionKey = "ssl.version"
	)

	var (
		buildVersion = bootstrapDump.GetBootstrap().GetNode().GetUserAgentBuildVersion()
		version      = buildVersion.GetVersion()
		md           = buildVersion.GetMetadata().GetFields()

		sb strings.Builder
	)

	fmt.Fprintf(&sb, "%d.%d.%d", version.GetMajorNumber(), version.GetMinorNumber(), version.GetPatch())
	if label, ok := md[buildLabelKey]; ok {
		fmt.Fprintf(&sb, "-%s", label.GetStringValue())
	}
	if status, ok := md[statusKey]; ok {
		fmt.Fprintf(&sb, "/%s", status.GetStringValue())
	}
	if typ, ok := md[buildTypeKey]; ok {
		fmt.Fprintf(&sb, "/%s", typ.GetStringValue())
	}
	if sslVersion, ok := md[sslVersionKey]; ok {
		fmt.Fprintf(&sb, "/%s", sslVersion.GetStringValue())
	}

	return sb.String()
}
