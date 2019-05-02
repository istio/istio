/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/renderutil"
	storageerrors "k8s.io/helm/pkg/storage/errors"
)

const upgradeDesc = `
This command upgrades a release to a specified version of a chart and/or updates chart values.

Required arguments are release and chart. The chart argument can be one of:
 - a chart reference('stable/mariadb'); use '--version' and '--devel' flags for versions other than latest,
 - a path to a chart directory,
 - a packaged chart,
 - a fully qualified URL.

To customize the chart values, use any of
 - '--values'/'-f' to pass in a yaml file holding settings,
 - '--set' to provide one or more key=val pairs directly,
 - '--set-string' to provide key=val forcing val to be stored as a string,
 - '--set-file' to provide key=path to read a single large value from a file at path.

To edit or append to the existing customized values, add the
 '--reuse-values' flag, otherwise any existing customized values are ignored.

If no chart value arguments are provided on the command line, any existing customized values are carried
forward. If you want to revert to just the values provided in the chart, use the '--reset-values' flag.

You can specify any of the chart value flags multiple times. The priority will be given to the last
(right-most) value specified. For example, if both myvalues.yaml and override.yaml contained a key
called 'Test', the value set in override.yaml would take precedence:

	$ helm upgrade -f myvalues.yaml -f override.yaml redis ./redis

Note that the key name provided to the '--set', '--set-string' and '--set-file' flags can reference
structure elements. Examples:
  - mybool=TRUE
  - livenessProbe.timeoutSeconds=10
  - metrics.annotations[0]=hey,metrics.annotations[1]=ho

which sets the top level key mybool to true, the nested timeoutSeconds to 10, and two array values, respectively.

Note that the value side of the key=val provided to '--set' and '--set-string' flags will pass through
shell evaluation followed by yaml type parsing to produce the final value. This may alter inputs with
special characters in unexpected ways, for example

	$ helm upgrade --set pwd=3jk$o2,z=f\30.e redis ./redis

results in "pwd: 3jk" and "z: f30.e". Use single quotes to avoid shell evaluation and argument delimiters,
and use backslash to escape yaml special characters:

	$ helm upgrade --set pwd='3jk$o2z=f\\30.e' redis ./redis

which results in the expected "pwd: 3jk$o2z=f\30.e". If a single quote occurs in your value then follow
your shell convention for escaping it; for example in bash:

	$ helm upgrade --set pwd='3jk$o2z=f\\30with'\''quote'

which results in "pwd: 3jk$o2z=f\30with'quote".
`

type upgradeCmd struct {
	release      string
	chart        string
	out          io.Writer
	client       helm.Interface
	dryRun       bool
	recreate     bool
	force        bool
	disableHooks bool
	valueFiles   valueFiles
	values       []string
	stringValues []string
	fileValues   []string
	verify       bool
	keyring      string
	install      bool
	namespace    string
	version      string
	timeout      int64
	resetValues  bool
	reuseValues  bool
	wait         bool
	atomic       bool
	repoURL      string
	username     string
	password     string
	devel        bool
	subNotes     bool
	description  string

	certFile string
	keyFile  string
	caFile   string
}

func newUpgradeCmd(client helm.Interface, out io.Writer) *cobra.Command {

	upgrade := &upgradeCmd{
		out:    out,
		client: client,
	}

	cmd := &cobra.Command{
		Use:     "upgrade [RELEASE] [CHART]",
		Short:   "upgrade a release",
		Long:    upgradeDesc,
		PreRunE: func(_ *cobra.Command, _ []string) error { return setupConnection() },
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkArgsLength(len(args), "release name", "chart path"); err != nil {
				return err
			}

			if upgrade.version == "" && upgrade.devel {
				debug("setting version to >0.0.0-0")
				upgrade.version = ">0.0.0-0"
			}

			upgrade.release = args[0]
			upgrade.chart = args[1]
			upgrade.client = ensureHelmClient(upgrade.client)
			upgrade.wait = upgrade.wait || upgrade.atomic

			return upgrade.run()
		},
	}

	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.VarP(&upgrade.valueFiles, "values", "f", "specify values in a YAML file or a URL(can specify multiple)")
	f.BoolVar(&upgrade.dryRun, "dry-run", false, "simulate an upgrade")
	f.BoolVar(&upgrade.recreate, "recreate-pods", false, "performs pods restart for the resource if applicable")
	f.BoolVar(&upgrade.force, "force", false, "force resource update through delete/recreate if needed")
	f.StringArrayVar(&upgrade.values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&upgrade.stringValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&upgrade.fileValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
	f.BoolVar(&upgrade.disableHooks, "disable-hooks", false, "disable pre/post upgrade hooks. DEPRECATED. Use no-hooks")
	f.BoolVar(&upgrade.disableHooks, "no-hooks", false, "disable pre/post upgrade hooks")
	f.BoolVar(&upgrade.verify, "verify", false, "verify the provenance of the chart before upgrading")
	f.StringVar(&upgrade.keyring, "keyring", defaultKeyring(), "path to the keyring that contains public signing keys")
	f.BoolVarP(&upgrade.install, "install", "i", false, "if a release by this name doesn't already exist, run an install")
	f.StringVar(&upgrade.namespace, "namespace", "", "namespace to install the release into (only used if --install is set). Defaults to the current kube config namespace")
	f.StringVar(&upgrade.version, "version", "", "specify the exact chart version to use. If this is not specified, the latest version is used")
	f.Int64Var(&upgrade.timeout, "timeout", 300, "time in seconds to wait for any individual Kubernetes operation (like Jobs for hooks)")
	f.BoolVar(&upgrade.resetValues, "reset-values", false, "when upgrading, reset the values to the ones built into the chart")
	f.BoolVar(&upgrade.reuseValues, "reuse-values", false, "when upgrading, reuse the last release's values and merge in any overrides from the command line via --set and -f. If '--reset-values' is specified, this is ignored.")
	f.BoolVar(&upgrade.wait, "wait", false, "if set, will wait until all Pods, PVCs, Services, and minimum number of Pods of a Deployment are in a ready state before marking the release as successful. It will wait for as long as --timeout")
	f.BoolVar(&upgrade.atomic, "atomic", false, "if set, upgrade process rolls back changes made in case of failed upgrade, also sets --wait flag")
	f.StringVar(&upgrade.repoURL, "repo", "", "chart repository url where to locate the requested chart")
	f.StringVar(&upgrade.username, "username", "", "chart repository username where to locate the requested chart")
	f.StringVar(&upgrade.password, "password", "", "chart repository password where to locate the requested chart")
	f.StringVar(&upgrade.certFile, "cert-file", "", "identify HTTPS client using this SSL certificate file")
	f.StringVar(&upgrade.keyFile, "key-file", "", "identify HTTPS client using this SSL key file")
	f.StringVar(&upgrade.caFile, "ca-file", "", "verify certificates of HTTPS-enabled servers using this CA bundle")
	f.BoolVar(&upgrade.devel, "devel", false, "use development versions, too. Equivalent to version '>0.0.0-0'. If --version is set, this is ignored.")
	f.BoolVar(&upgrade.subNotes, "render-subchart-notes", false, "render subchart notes along with parent")
	f.StringVar(&upgrade.description, "description", "", "specify the description to use for the upgrade, rather than the default")

	f.MarkDeprecated("disable-hooks", "use --no-hooks instead")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

func (u *upgradeCmd) run() error {
	chartPath, err := locateChartPath(u.repoURL, u.username, u.password, u.chart, u.version, u.verify, u.keyring, u.certFile, u.keyFile, u.caFile)
	if err != nil {
		return err
	}

	releaseHistory, err := u.client.ReleaseHistory(u.release, helm.WithMaxHistory(1))

	if u.install {
		// If a release does not exist, install it. If another error occurs during
		// the check, ignore the error and continue with the upgrade.
		//
		// The returned error is a grpc.rpcError that wraps the message from the original error.
		// So we're stuck doing string matching against the wrapped error, which is nested somewhere
		// inside of the grpc.rpcError message.

		if err == nil {
			if u.namespace == "" {
				u.namespace = defaultNamespace()
			}
			previousReleaseNamespace := releaseHistory.Releases[0].Namespace
			if previousReleaseNamespace != u.namespace {
				fmt.Fprintf(u.out,
					"WARNING: Namespace %q doesn't match with previous. Release will be deployed to %s\n",
					u.namespace, previousReleaseNamespace,
				)
			}
		}

		if err != nil && strings.Contains(err.Error(), storageerrors.ErrReleaseNotFound(u.release).Error()) {
			fmt.Fprintf(u.out, "Release %q does not exist. Installing it now.\n", u.release)
			ic := &installCmd{
				chartPath:    chartPath,
				client:       u.client,
				out:          u.out,
				name:         u.release,
				valueFiles:   u.valueFiles,
				dryRun:       u.dryRun,
				verify:       u.verify,
				disableHooks: u.disableHooks,
				keyring:      u.keyring,
				values:       u.values,
				stringValues: u.stringValues,
				fileValues:   u.fileValues,
				namespace:    u.namespace,
				timeout:      u.timeout,
				wait:         u.wait,
				description:  u.description,
				atomic:       u.atomic,
			}
			return ic.run()
		}
	}

	rawVals, err := vals(u.valueFiles, u.values, u.stringValues, u.fileValues, u.certFile, u.keyFile, u.caFile)
	if err != nil {
		return err
	}

	// Check chart requirements to make sure all dependencies are present in /charts
	if ch, err := chartutil.Load(chartPath); err == nil {
		if req, err := chartutil.LoadRequirements(ch); err == nil {
			if err := renderutil.CheckDependencies(ch, req); err != nil {
				return err
			}
		} else if err != chartutil.ErrRequirementsNotFound {
			return fmt.Errorf("cannot load requirements: %v", err)
		}
	} else {
		return prettyError(err)
	}

	resp, err := u.client.UpdateRelease(
		u.release,
		chartPath,
		helm.UpdateValueOverrides(rawVals),
		helm.UpgradeDryRun(u.dryRun),
		helm.UpgradeRecreate(u.recreate),
		helm.UpgradeForce(u.force),
		helm.UpgradeDisableHooks(u.disableHooks),
		helm.UpgradeTimeout(u.timeout),
		helm.ResetValues(u.resetValues),
		helm.ReuseValues(u.reuseValues),
		helm.UpgradeSubNotes(u.subNotes),
		helm.UpgradeWait(u.wait),
		helm.UpgradeDescription(u.description))
	if err != nil {
		fmt.Fprintf(u.out, "UPGRADE FAILED\nROLLING BACK\nError: %v\n", prettyError(err))
		if u.atomic {
			rollback := &rollbackCmd{
				out:          u.out,
				client:       u.client,
				name:         u.release,
				dryRun:       u.dryRun,
				recreate:     u.recreate,
				force:        u.force,
				timeout:      u.timeout,
				wait:         u.wait,
				description:  "",
				revision:     releaseHistory.Releases[0].Version,
				disableHooks: u.disableHooks,
			}
			if err := rollback.run(); err != nil {
				return err
			}
		}
		return fmt.Errorf("UPGRADE FAILED: %v", prettyError(err))
	}

	if settings.Debug {
		printRelease(u.out, resp.Release)
	}

	fmt.Fprintf(u.out, "Release %q has been upgraded. Happy Helming!\n", u.release)

	// Print the status like status command does
	status, err := u.client.ReleaseStatus(u.release)
	if err != nil {
		return prettyError(err)
	}
	PrintStatus(u.out, status)

	return nil
}
