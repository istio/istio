// Copyright 2019 The Operator-SDK Authors
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

package scorecard

import (
	"fmt"
	"strings"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/version"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// scorecardConfig stores all scorecard config passed as flags
type scorecardConfig struct {
	namespace          string
	kubeconfigPath     string
	initTimeout        int
	olmDeployed        bool
	csvPath            string
	basicTests         bool
	olmTests           bool
	tenantTests        bool
	namespacedManifest string
	globalManifest     string
	crManifest         string
	proxyImage         string
	proxyPullPolicy    string
	crdsDir            string
	verbose            bool
}

var scConf scorecardConfig

func NewCmd() *cobra.Command {
	scorecardCmd := &cobra.Command{
		Use:   "scorecard",
		Short: "Run scorecard tests",
		Long: `Runs blackbox scorecard tests on an operator
`,
		RunE: ScorecardTests,
	}

	scorecardCmd.Flags().StringVar(&ScorecardConf, ConfigOpt, "", "config file (default is <project_dir>/.osdk-yaml)")
	scorecardCmd.Flags().StringVar(&scConf.namespace, NamespaceOpt, "", "Namespace of custom resource created in cluster")
	scorecardCmd.Flags().StringVar(&scConf.kubeconfigPath, KubeconfigOpt, "", "Path to kubeconfig of custom resource created in cluster")
	scorecardCmd.Flags().IntVar(&scConf.initTimeout, InitTimeoutOpt, 10, "Timeout for status block on CR to be created in seconds")
	scorecardCmd.Flags().BoolVar(&scConf.olmDeployed, OlmDeployedOpt, false, "The OLM has deployed the operator. Use only the CSV for test data")
	scorecardCmd.Flags().StringVar(&scConf.csvPath, CSVPathOpt, "", "Path to CSV being tested")
	scorecardCmd.Flags().BoolVar(&scConf.basicTests, BasicTestsOpt, true, "Enable basic operator checks")
	scorecardCmd.Flags().BoolVar(&scConf.olmTests, OLMTestsOpt, true, "Enable OLM integration checks")
	scorecardCmd.Flags().BoolVar(&scConf.tenantTests, TenantTestsOpt, false, "Enable good tenant checks")
	scorecardCmd.Flags().StringVar(&scConf.namespacedManifest, NamespacedManifestOpt, "", "Path to manifest for namespaced resources (e.g. RBAC and Operator manifest)")
	scorecardCmd.Flags().StringVar(&scConf.globalManifest, GlobalManifestOpt, "", "Path to manifest for Global resources (e.g. CRD manifests)")
	scorecardCmd.Flags().StringVar(&scConf.crManifest, CRManifestOpt, "", "Path to manifest for Custom Resource (required)")
	scorecardCmd.Flags().StringVar(&scConf.proxyImage, ProxyImageOpt, fmt.Sprintf("quay.io/operator-framework/scorecard-proxy:%s", strings.TrimSuffix(version.Version, "+git")), "Image name for scorecard proxy")
	scorecardCmd.Flags().StringVar(&scConf.proxyPullPolicy, ProxyPullPolicyOpt, "Always", "Pull policy for scorecard proxy image")
	scorecardCmd.Flags().StringVar(&scConf.crdsDir, "crds-dir", scaffold.CRDsDir, "Directory containing CRDs (all CRD manifest filenames must have the suffix 'crd.yaml')")
	scorecardCmd.Flags().BoolVar(&scConf.verbose, VerboseOpt, false, "Enable verbose logging")

	if err := viper.BindPFlags(scorecardCmd.Flags()); err != nil {
		log.Fatalf("Failed to bind scorecard flags to viper: %v", err)
	}

	return scorecardCmd
}
