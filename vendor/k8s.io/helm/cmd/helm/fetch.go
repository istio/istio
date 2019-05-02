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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/repo"
)

const fetchDesc = `
Retrieve a package from a package repository, and download it locally.

This is useful for fetching packages to inspect, modify, or repackage. It can
also be used to perform cryptographic verification of a chart without installing
the chart.

There are options for unpacking the chart after download. This will create a
directory for the chart and uncompress into that directory.

If the --verify flag is specified, the requested chart MUST have a provenance
file, and MUST pass the verification process. Failure in any part of this will
result in an error, and the chart will not be saved locally.
`

type fetchCmd struct {
	untar    bool
	untardir string
	chartRef string
	destdir  string
	version  string
	repoURL  string
	username string
	password string

	verify      bool
	verifyLater bool
	keyring     string

	certFile string
	keyFile  string
	caFile   string

	devel bool

	out io.Writer
}

func newFetchCmd(out io.Writer) *cobra.Command {
	fch := &fetchCmd{out: out}

	cmd := &cobra.Command{
		Use:   "fetch [flags] [chart URL | repo/chartname] [...]",
		Short: "download a chart from a repository and (optionally) unpack it in local directory",
		Long:  fetchDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("need at least one argument, url or repo/name of the chart")
			}

			if fch.version == "" && fch.devel {
				debug("setting version to >0.0.0-0")
				fch.version = ">0.0.0-0"
			}

			for i := 0; i < len(args); i++ {
				fch.chartRef = args[i]
				if err := fch.run(); err != nil {
					return err
				}
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&fch.untar, "untar", false, "if set to true, will untar the chart after downloading it")
	f.StringVar(&fch.untardir, "untardir", ".", "if untar is specified, this flag specifies the name of the directory into which the chart is expanded")
	f.BoolVar(&fch.verify, "verify", false, "verify the package against its signature")
	f.BoolVar(&fch.verifyLater, "prov", false, "fetch the provenance file, but don't perform verification")
	f.StringVar(&fch.version, "version", "", "specific version of a chart. Without this, the latest version is fetched")
	f.StringVar(&fch.keyring, "keyring", defaultKeyring(), "keyring containing public keys")
	f.StringVarP(&fch.destdir, "destination", "d", ".", "location to write the chart. If this and tardir are specified, tardir is appended to this")
	f.StringVar(&fch.repoURL, "repo", "", "chart repository url where to locate the requested chart")
	f.StringVar(&fch.certFile, "cert-file", "", "identify HTTPS client using this SSL certificate file")
	f.StringVar(&fch.keyFile, "key-file", "", "identify HTTPS client using this SSL key file")
	f.StringVar(&fch.caFile, "ca-file", "", "verify certificates of HTTPS-enabled servers using this CA bundle")
	f.BoolVar(&fch.devel, "devel", false, "use development versions, too. Equivalent to version '>0.0.0-0'. If --version is set, this is ignored.")
	f.StringVar(&fch.username, "username", "", "chart repository username")
	f.StringVar(&fch.password, "password", "", "chart repository password")

	return cmd
}

func (f *fetchCmd) run() error {
	c := downloader.ChartDownloader{
		HelmHome: settings.Home,
		Out:      f.out,
		Keyring:  f.keyring,
		Verify:   downloader.VerifyNever,
		Getters:  getter.All(settings),
		Username: f.username,
		Password: f.password,
	}

	if f.verify {
		c.Verify = downloader.VerifyAlways
	} else if f.verifyLater {
		c.Verify = downloader.VerifyLater
	}

	// If untar is set, we fetch to a tempdir, then untar and copy after
	// verification.
	dest := f.destdir
	if f.untar {
		var err error
		dest, err = ioutil.TempDir("", "helm-")
		if err != nil {
			return fmt.Errorf("Failed to untar: %s", err)
		}
		defer os.RemoveAll(dest)
	}

	if f.repoURL != "" {
		chartURL, err := repo.FindChartInAuthRepoURL(f.repoURL, f.username, f.password, f.chartRef, f.version, f.certFile, f.keyFile, f.caFile, getter.All(settings))
		if err != nil {
			return err
		}
		f.chartRef = chartURL
	}

	saved, v, err := c.DownloadTo(f.chartRef, f.version, dest)
	if err != nil {
		return err
	}

	if f.verify {
		fmt.Fprintf(f.out, "Verification: %v\n", v)
	}

	// After verification, untar the chart into the requested directory.
	if f.untar {
		ud := f.untardir
		if !filepath.IsAbs(ud) {
			ud = filepath.Join(f.destdir, ud)
		}
		if fi, err := os.Stat(ud); err != nil {
			if err := os.MkdirAll(ud, 0755); err != nil {
				return fmt.Errorf("Failed to untar (mkdir): %s", err)
			}

		} else if !fi.IsDir() {
			return fmt.Errorf("Failed to untar: %s is not a directory", ud)
		}

		return chartutil.ExpandFile(ud, saved)
	}
	return nil
}

// defaultKeyring returns the expanded path to the default keyring.
func defaultKeyring() string {
	return os.ExpandEnv("$HOME/.gnupg/pubring.gpg")
}
