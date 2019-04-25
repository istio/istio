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
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/kube"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/repo"
	"k8s.io/helm/pkg/strvals"
)

const installDesc = `
This command installs a chart archive.

The install argument must be a chart reference, a path to a packaged chart,
a path to an unpacked chart directory or a URL.

To override values in a chart, use either the '--values' flag and pass in a file
or use the '--set' flag and pass configuration from the command line.  To force string
values in '--set', use '--set-string' instead. In case a value is large and therefore
you want not to use neither '--values' nor '--set', use '--set-file' to read the
single large value from file.

	$ helm install -f myvalues.yaml ./redis

or

	$ helm install --set name=prod ./redis

or

	$ helm install --set-string long_int=1234567890 ./redis

or
    $ helm install --set-file multiline_text=path/to/textfile

You can specify the '--values'/'-f' flag multiple times. The priority will be given to the
last (right-most) file specified. For example, if both myvalues.yaml and override.yaml
contained a key called 'Test', the value set in override.yaml would take precedence:

	$ helm install -f myvalues.yaml -f override.yaml ./redis

You can specify the '--set' flag multiple times. The priority will be given to the
last (right-most) set specified. For example, if both 'bar' and 'newbar' values are
set for a key called 'foo', the 'newbar' value would take precedence:

	$ helm install --set foo=bar --set foo=newbar ./redis


To check the generated manifests of a release without installing the chart,
the '--debug' and '--dry-run' flags can be combined. This will still require a
round-trip to the Tiller server.

If --verify is set, the chart MUST have a provenance file, and the provenance
file MUST pass all verification steps.

There are five different ways you can express the chart you want to install:

1. By chart reference: helm install stable/mariadb
2. By path to a packaged chart: helm install ./nginx-1.2.3.tgz
3. By path to an unpacked chart directory: helm install ./nginx
4. By absolute URL: helm install https://example.com/charts/nginx-1.2.3.tgz
5. By chart reference and repo url: helm install --repo https://example.com/charts/ nginx

CHART REFERENCES

A chart reference is a convenient way of reference a chart in a chart repository.

When you use a chart reference with a repo prefix ('stable/mariadb'), Helm will look in the local
configuration for a chart repository named 'stable', and will then look for a
chart in that repository whose name is 'mariadb'. It will install the latest
version of that chart unless you also supply a version number with the
'--version' flag.

To see the list of chart repositories, use 'helm repo list'. To search for
charts in a repository, use 'helm search'.
`

type installCmd struct {
	name           string
	namespace      string
	valueFiles     valueFiles
	chartPath      string
	dryRun         bool
	disableHooks   bool
	disableCRDHook bool
	replace        bool
	verify         bool
	keyring        string
	out            io.Writer
	client         helm.Interface
	values         []string
	stringValues   []string
	fileValues     []string
	nameTemplate   string
	version        string
	timeout        int64
	wait           bool
	atomic         bool
	repoURL        string
	username       string
	password       string
	devel          bool
	depUp          bool
	subNotes       bool
	description    string

	certFile string
	keyFile  string
	caFile   string
}

type valueFiles []string

func (v *valueFiles) String() string {
	return fmt.Sprint(*v)
}

func (v *valueFiles) Type() string {
	return "valueFiles"
}

func (v *valueFiles) Set(value string) error {
	for _, filePath := range strings.Split(value, ",") {
		*v = append(*v, filePath)
	}
	return nil
}

func newInstallCmd(c helm.Interface, out io.Writer) *cobra.Command {
	inst := &installCmd{
		out:    out,
		client: c,
	}

	cmd := &cobra.Command{
		Use:     "install [CHART]",
		Short:   "install a chart archive",
		Long:    installDesc,
		PreRunE: func(_ *cobra.Command, _ []string) error { return setupConnection() },
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkArgsLength(len(args), "chart name"); err != nil {
				return err
			}

			debug("Original chart version: %q", inst.version)
			if inst.version == "" && inst.devel {
				debug("setting version to >0.0.0-0")
				inst.version = ">0.0.0-0"
			}

			cp, err := locateChartPath(inst.repoURL, inst.username, inst.password, args[0], inst.version, inst.verify, inst.keyring,
				inst.certFile, inst.keyFile, inst.caFile)
			if err != nil {
				return err
			}
			inst.chartPath = cp
			inst.client = ensureHelmClient(inst.client)
			inst.wait = inst.wait || inst.atomic

			return inst.run()
		},
	}

	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.VarP(&inst.valueFiles, "values", "f", "specify values in a YAML file or a URL(can specify multiple)")
	f.StringVarP(&inst.name, "name", "n", "", "release name. If unspecified, it will autogenerate one for you")
	f.StringVar(&inst.namespace, "namespace", "", "namespace to install the release into. Defaults to the current kube config namespace.")
	f.BoolVar(&inst.dryRun, "dry-run", false, "simulate an install")
	f.BoolVar(&inst.disableHooks, "no-hooks", false, "prevent hooks from running during install")
	f.BoolVar(&inst.disableCRDHook, "no-crd-hook", false, "prevent CRD hooks from running, but run other hooks")
	f.BoolVar(&inst.replace, "replace", false, "re-use the given name, even if that name is already used. This is unsafe in production")
	f.StringArrayVar(&inst.values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&inst.stringValues, "set-string", []string{}, "set STRING values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")
	f.StringArrayVar(&inst.fileValues, "set-file", []string{}, "set values from respective files specified via the command line (can specify multiple or separate values with commas: key1=path1,key2=path2)")
	f.StringVar(&inst.nameTemplate, "name-template", "", "specify template used to name the release")
	f.BoolVar(&inst.verify, "verify", false, "verify the package before installing it")
	f.StringVar(&inst.keyring, "keyring", defaultKeyring(), "location of public keys used for verification")
	f.StringVar(&inst.version, "version", "", "specify the exact chart version to install. If this is not specified, the latest version is installed")
	f.Int64Var(&inst.timeout, "timeout", 300, "time in seconds to wait for any individual Kubernetes operation (like Jobs for hooks)")
	f.BoolVar(&inst.wait, "wait", false, "if set, will wait until all Pods, PVCs, Services, and minimum number of Pods of a Deployment are in a ready state before marking the release as successful. It will wait for as long as --timeout")
	f.BoolVar(&inst.atomic, "atomic", false, "if set, installation process purges chart on fail, also sets --wait flag")
	f.StringVar(&inst.repoURL, "repo", "", "chart repository url where to locate the requested chart")
	f.StringVar(&inst.username, "username", "", "chart repository username where to locate the requested chart")
	f.StringVar(&inst.password, "password", "", "chart repository password where to locate the requested chart")
	f.StringVar(&inst.certFile, "cert-file", "", "identify HTTPS client using this SSL certificate file")
	f.StringVar(&inst.keyFile, "key-file", "", "identify HTTPS client using this SSL key file")
	f.StringVar(&inst.caFile, "ca-file", "", "verify certificates of HTTPS-enabled servers using this CA bundle")
	f.BoolVar(&inst.devel, "devel", false, "use development versions, too. Equivalent to version '>0.0.0-0'. If --version is set, this is ignored.")
	f.BoolVar(&inst.depUp, "dep-up", false, "run helm dependency update before installing the chart")
	f.BoolVar(&inst.subNotes, "render-subchart-notes", false, "render subchart notes along with the parent")
	f.StringVar(&inst.description, "description", "", "specify a description for the release")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

func (i *installCmd) run() error {
	debug("CHART PATH: %s\n", i.chartPath)

	if i.namespace == "" {
		i.namespace = defaultNamespace()
	}

	rawVals, err := vals(i.valueFiles, i.values, i.stringValues, i.fileValues, i.certFile, i.keyFile, i.caFile)
	if err != nil {
		return err
	}

	// If template is specified, try to run the template.
	if i.nameTemplate != "" {
		i.name, err = generateName(i.nameTemplate)
		if err != nil {
			return err
		}
		// Print the final name so the user knows what the final name of the release is.
		fmt.Printf("FINAL NAME: %s\n", i.name)
	}

	if msgs := validation.IsDNS1123Subdomain(i.name); i.name != "" && len(msgs) > 0 {
		return fmt.Errorf("release name %s is invalid: %s", i.name, strings.Join(msgs, ";"))
	}

	// Check chart requirements to make sure all dependencies are present in /charts
	chartRequested, err := chartutil.Load(i.chartPath)
	if err != nil {
		return prettyError(err)
	}

	if req, err := chartutil.LoadRequirements(chartRequested); err == nil {
		// If checkDependencies returns an error, we have unfulfilled dependencies.
		// As of Helm 2.4.0, this is treated as a stopping condition:
		// https://github.com/kubernetes/helm/issues/2209
		if err := renderutil.CheckDependencies(chartRequested, req); err != nil {
			if i.depUp {
				man := &downloader.Manager{
					Out:        i.out,
					ChartPath:  i.chartPath,
					HelmHome:   settings.Home,
					Keyring:    defaultKeyring(),
					SkipUpdate: false,
					Getters:    getter.All(settings),
				}
				if err := man.Update(); err != nil {
					return prettyError(err)
				}

				// Update all dependencies which are present in /charts.
				chartRequested, err = chartutil.Load(i.chartPath)
				if err != nil {
					return prettyError(err)
				}
			} else {
				return prettyError(err)
			}

		}
	} else if err != chartutil.ErrRequirementsNotFound {
		return fmt.Errorf("cannot load requirements: %v", err)
	}

	res, err := i.client.InstallReleaseFromChart(
		chartRequested,
		i.namespace,
		helm.ValueOverrides(rawVals),
		helm.ReleaseName(i.name),
		helm.InstallDryRun(i.dryRun),
		helm.InstallReuseName(i.replace),
		helm.InstallDisableHooks(i.disableHooks),
		helm.InstallDisableCRDHook(i.disableCRDHook),
		helm.InstallSubNotes(i.subNotes),
		helm.InstallTimeout(i.timeout),
		helm.InstallWait(i.wait),
		helm.InstallDescription(i.description))
	if err != nil {
		if i.atomic {
			fmt.Fprintf(os.Stdout, "INSTALL FAILED\nPURGING CHART\nError: %v\n", prettyError(err))
			deleteSideEffects := &deleteCmd{
				name:         i.name,
				disableHooks: i.disableHooks,
				purge:        true,
				timeout:      i.timeout,
				description:  "",
				dryRun:       i.dryRun,
				out:          i.out,
				client:       i.client,
			}
			if err := deleteSideEffects.run(); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "Successfully purged a chart!\n")
		}
		return prettyError(err)
	}

	rel := res.GetRelease()
	if rel == nil {
		return nil
	}
	i.printRelease(rel)

	// If this is a dry run, we can't display status.
	if i.dryRun {
		// This is special casing to avoid breaking backward compatibility:
		if res.Release.Info.Description != "Dry run complete" {
			fmt.Fprintf(os.Stdout, "WARNING: %s\n", res.Release.Info.Description)
		}
		return nil
	}

	// Print the status like status command does
	status, err := i.client.ReleaseStatus(rel.Name)
	if err != nil {
		return prettyError(err)
	}
	PrintStatus(i.out, status)
	return nil
}

// Merges source and destination map, preferring values from the source map
func mergeValues(dest map[string]interface{}, src map[string]interface{}) map[string]interface{} {
	for k, v := range src {
		// If the key doesn't exist already, then just set the key to that value
		if _, exists := dest[k]; !exists {
			dest[k] = v
			continue
		}
		nextMap, ok := v.(map[string]interface{})
		// If it isn't another map, overwrite the value
		if !ok {
			dest[k] = v
			continue
		}
		// Edge case: If the key exists in the destination, but isn't a map
		destMap, isMap := dest[k].(map[string]interface{})
		// If the source map has a map for this key, prefer it
		if !isMap {
			dest[k] = v
			continue
		}
		// If we got to this point, it is a map in both, so merge them
		dest[k] = mergeValues(destMap, nextMap)
	}
	return dest
}

// vals merges values from files specified via -f/--values and
// directly via --set or --set-string or --set-file, marshaling them to YAML
func vals(valueFiles valueFiles, values []string, stringValues []string, fileValues []string, CertFile, KeyFile, CAFile string) ([]byte, error) {
	base := map[string]interface{}{}

	// User specified a values files via -f/--values
	for _, filePath := range valueFiles {
		currentMap := map[string]interface{}{}

		var bytes []byte
		var err error
		if strings.TrimSpace(filePath) == "-" {
			bytes, err = ioutil.ReadAll(os.Stdin)
		} else {
			bytes, err = readFile(filePath, CertFile, KeyFile, CAFile)
		}

		if err != nil {
			return []byte{}, err
		}

		if err := yaml.Unmarshal(bytes, &currentMap); err != nil {
			return []byte{}, fmt.Errorf("failed to parse %s: %s", filePath, err)
		}
		// Merge with the previous map
		base = mergeValues(base, currentMap)
	}

	// User specified a value via --set
	for _, value := range values {
		if err := strvals.ParseInto(value, base); err != nil {
			return []byte{}, fmt.Errorf("failed parsing --set data: %s", err)
		}
	}

	// User specified a value via --set-string
	for _, value := range stringValues {
		if err := strvals.ParseIntoString(value, base); err != nil {
			return []byte{}, fmt.Errorf("failed parsing --set-string data: %s", err)
		}
	}

	// User specified a value via --set-file
	for _, value := range fileValues {
		reader := func(rs []rune) (interface{}, error) {
			bytes, err := readFile(string(rs), CertFile, KeyFile, CAFile)
			return string(bytes), err
		}
		if err := strvals.ParseIntoFile(value, base, reader); err != nil {
			return []byte{}, fmt.Errorf("failed parsing --set-file data: %s", err)
		}
	}

	return yaml.Marshal(base)
}

// printRelease prints info about a release if the Debug is true.
func (i *installCmd) printRelease(rel *release.Release) {
	if rel == nil {
		return
	}
	// TODO: Switch to text/template like everything else.
	fmt.Fprintf(i.out, "NAME:   %s\n", rel.Name)
	if settings.Debug {
		printRelease(i.out, rel)
	}
}

// locateChartPath looks for a chart directory in known places, and returns either the full path or an error.
//
// This does not ensure that the chart is well-formed; only that the requested filename exists.
//
// Order of resolution:
// - current working directory
// - if path is absolute or begins with '.', error out here
// - chart repos in $HELM_HOME
// - URL
//
// If 'verify' is true, this will attempt to also verify the chart.
func locateChartPath(repoURL, username, password, name, version string, verify bool, keyring,
	certFile, keyFile, caFile string) (string, error) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if fi, err := os.Stat(name); err == nil {
		abs, err := filepath.Abs(name)
		if err != nil {
			return abs, err
		}
		if verify {
			if fi.IsDir() {
				return "", errors.New("cannot verify a directory")
			}
			if _, err := downloader.VerifyChart(abs, keyring); err != nil {
				return "", err
			}
		}
		return abs, nil
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, ".") {
		return name, fmt.Errorf("path %q not found", name)
	}

	crepo := filepath.Join(settings.Home.Repository(), name)
	if _, err := os.Stat(crepo); err == nil {
		return filepath.Abs(crepo)
	}

	dl := downloader.ChartDownloader{
		HelmHome: settings.Home,
		Out:      os.Stdout,
		Keyring:  keyring,
		Getters:  getter.All(settings),
		Username: username,
		Password: password,
	}
	if verify {
		dl.Verify = downloader.VerifyAlways
	}
	if repoURL != "" {
		chartURL, err := repo.FindChartInAuthRepoURL(repoURL, username, password, name, version,
			certFile, keyFile, caFile, getter.All(settings))
		if err != nil {
			return "", err
		}
		name = chartURL
	}

	if _, err := os.Stat(settings.Home.Archive()); os.IsNotExist(err) {
		os.MkdirAll(settings.Home.Archive(), 0744)
	}

	filename, _, err := dl.DownloadTo(name, version, settings.Home.Archive())
	if err == nil {
		lname, err := filepath.Abs(filename)
		if err != nil {
			return filename, err
		}
		debug("Fetched %s to %s\n", name, filename)
		return lname, nil
	} else if settings.Debug {
		return filename, err
	}

	return filename, fmt.Errorf("failed to download %q (hint: running `helm repo update` may help)", name)
}

func generateName(nameTemplate string) (string, error) {
	t, err := template.New("name-template").Funcs(sprig.TxtFuncMap()).Parse(nameTemplate)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	err = t.Execute(&b, nil)
	if err != nil {
		return "", err
	}
	return b.String(), nil
}

func defaultNamespace() string {
	if ns, _, err := kube.GetConfig(settings.KubeContext, settings.KubeConfig).Namespace(); err == nil {
		return ns
	}
	return "default"
}

//readFile load a file from the local directory or a remote file with a url.
func readFile(filePath, CertFile, KeyFile, CAFile string) ([]byte, error) {
	u, _ := url.Parse(filePath)
	p := getter.All(settings)

	// FIXME: maybe someone handle other protocols like ftp.
	getterConstructor, err := p.ByScheme(u.Scheme)

	if err != nil {
		return ioutil.ReadFile(filePath)
	}

	getter, err := getterConstructor(filePath, CertFile, KeyFile, CAFile)
	if err != nil {
		return []byte{}, err
	}
	data, err := getter.Get(filePath)
	return data.Bytes(), err
}
