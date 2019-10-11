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

package mesh

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	"istio.io/operator/pkg/apis/istio/v1alpha2"
	"istio.io/operator/pkg/component/controlplane"
	"istio.io/operator/pkg/helm"
	"istio.io/operator/pkg/name"
	"istio.io/operator/pkg/tpath"
	"istio.io/operator/pkg/translate"
	"istio.io/operator/pkg/util"
	"istio.io/operator/pkg/validate"
	binversion "istio.io/operator/version"
)

// ManifestCmd is a group of commands related to manifest generation, installation, diffing and migration.
func ManifestCmd() *cobra.Command {
	mc := &cobra.Command{
		Use:   "manifest",
		Short: "Commands related to Istio manifests",
		Long:  "The manifest subcommand generates, applies, diffs or migrates Istio manifests.",
	}

	mgcArgs := &manifestGenerateArgs{}
	mdcArgs := &manifestDiffArgs{}
	macArgs := &manifestApplyArgs{}
	mvArgs := &manifestVersionsArgs{}
	mmcArgs := &manifestMigrateArgs{}

	args := &rootArgs{}

	mgc := manifestGenerateCmd(args, mgcArgs)
	mdc := manifestDiffCmd(args, mdcArgs)
	mac := manifestApplyCmd(args, macArgs)
	mvc := manifestVersionsCmd(args, mvArgs)
	mmc := manifestMigrateCmd(args, mmcArgs)

	addFlags(mc, args)
	addFlags(mgc, args)
	addFlags(mdc, args)
	addFlags(mac, args)
	addFlags(mvc, args)
	addFlags(mmc, args)

	addManifestGenerateFlags(mgc, mgcArgs)
	addManifestDiffFlags(mdc, mdcArgs)
	addManifestApplyFlags(mac, macArgs)
	addManifestVersionsFlags(mvc, mvArgs)
	addManifestMigrateFlags(mmc, mmcArgs)

	mc.AddCommand(mgc)
	mc.AddCommand(mdc)
	mc.AddCommand(mac)
	mc.AddCommand(mmc)
	mc.AddCommand(mvc)

	return mc
}

func genManifests(inFilename string, setOverlayYAML string, force bool, l *logger) (name.ManifestMap, error) {
	mergedYAML, err := genProfile(false, inFilename, "", setOverlayYAML, "", force, l)
	if err != nil {
		return nil, err
	}
	mergedICPS, err := unmarshalAndValidateICPS(mergedYAML, force, l)
	if err != nil {
		return nil, err
	}

	t, err := translate.NewTranslator(binversion.OperatorBinaryVersion.MinorVersion)
	if err != nil {
		return nil, err
	}

	if err := fetchInstallPackageFromURL(mergedICPS); err != nil {
		return nil, err
	}

	cp := controlplane.NewIstioControlPlane(mergedICPS, t)
	if err := cp.Run(); err != nil {
		return nil, fmt.Errorf("failed to create Istio control plane with spec: \n%v\nerror: %s", mergedICPS, err)
	}

	manifests, errs := cp.RenderManifest()
	if errs != nil {
		return manifests, errs.ToError()
	}
	return manifests, nil
}

// fetchInstallPackageFromURL downloads installation packages from specified URL.
func fetchInstallPackageFromURL(mergedICPS *v1alpha2.IstioControlPlaneSpec) error {
	if util.IsHTTPURL(mergedICPS.InstallPackagePath) {
		uf, err := helm.NewURLFetcher(mergedICPS.InstallPackagePath, "")
		if err != nil {
			return err
		}
		if err := uf.FetchBundles().ToError(); err != nil {
			return err
		}
		isp := path.Base(mergedICPS.InstallPackagePath)
		// get rid of the suffix, installation package is untared to folder name istio-{version}, e.g. istio-1.3.0
		idx := strings.LastIndex(isp, "-")
		// TODO: replace with more robust logic to set local file path
		mergedICPS.InstallPackagePath = filepath.Join(uf.DestDir(), isp[:idx], helm.ChartsFilePath)
	}
	return nil
}

// makeTreeFromSetList creates a YAML tree from a string slice containing key-value pairs in the format key=value.
func makeTreeFromSetList(setOverlay []string) (string, error) {
	if len(setOverlay) == 0 {
		return "", nil
	}
	tree := make(map[string]interface{})
	// Populate a default namespace for convenience, otherwise most --set commands will error out.
	if err := tpath.WriteNode(tree, util.PathFromString("defaultNamespace"), "istio-system"); err != nil {
		return "", err
	}
	for _, kv := range setOverlay {
		kvv := strings.Split(kv, "=")
		if len(kvv) != 2 {
			return "", fmt.Errorf("bad argument %s: expect format key=value", kv)
		}
		k := kvv[0]
		v := util.ParseValue(kvv[1])
		if err := tpath.WriteNode(tree, util.PathFromString(k), v); err != nil {
			return "", err
		}
		// To make errors more user friendly, test the path and error out immediately if we cannot unmarshal.
		testTree, err := yaml.Marshal(tree)
		if err != nil {
			return "", err
		}
		icps := &v1alpha2.IstioControlPlaneSpec{}
		if err := util.UnmarshalWithJSONPB(string(testTree), icps); err != nil {
			return "", fmt.Errorf("bad path=value: %s", kv)
		}
		if errs := validate.CheckIstioControlPlaneSpec(icps, true); len(errs) != 0 {
			return "", fmt.Errorf("bad path=value (%s): %s", kv, errs)
		}

	}
	out, err := yaml.Marshal(tree)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
