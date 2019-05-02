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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Masterminds/semver"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/terminal"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/downloader"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/provenance"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/repo"
)

const packageDesc = `
This command packages a chart into a versioned chart archive file. If a path
is given, this will look at that path for a chart (which must contain a
Chart.yaml file) and then package that directory.

If no path is given, this will look in the present working directory for a
Chart.yaml file, and (if found) build the current directory into a chart.

Versioned chart archives are used by Helm package repositories.
`

type packageCmd struct {
	save             bool
	sign             bool
	path             string
	key              string
	keyring          string
	version          string
	appVersion       string
	destination      string
	dependencyUpdate bool

	out  io.Writer
	home helmpath.Home
}

func newPackageCmd(out io.Writer) *cobra.Command {
	pkg := &packageCmd{out: out}

	cmd := &cobra.Command{
		Use:   "package [flags] [CHART_PATH] [...]",
		Short: "package a chart directory into a chart archive",
		Long:  packageDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg.home = settings.Home
			if len(args) == 0 {
				return fmt.Errorf("need at least one argument, the path to the chart")
			}
			if pkg.sign {
				if pkg.key == "" {
					return errors.New("--key is required for signing a package")
				}
				if pkg.keyring == "" {
					return errors.New("--keyring is required for signing a package")
				}
			}
			for i := 0; i < len(args); i++ {
				pkg.path = args[i]
				if err := pkg.run(); err != nil {
					return err
				}
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&pkg.save, "save", true, "save packaged chart to local chart repository")
	f.BoolVar(&pkg.sign, "sign", false, "use a PGP private key to sign this package")
	f.StringVar(&pkg.key, "key", "", "name of the key to use when signing. Used if --sign is true")
	f.StringVar(&pkg.keyring, "keyring", defaultKeyring(), "location of a public keyring")
	f.StringVar(&pkg.version, "version", "", "set the version on the chart to this semver version")
	f.StringVar(&pkg.appVersion, "app-version", "", "set the appVersion on the chart to this version")
	f.StringVarP(&pkg.destination, "destination", "d", ".", "location to write the chart.")
	f.BoolVarP(&pkg.dependencyUpdate, "dependency-update", "u", false, `update dependencies from "requirements.yaml" to dir "charts/" before packaging`)

	return cmd
}

func (p *packageCmd) run() error {
	path, err := filepath.Abs(p.path)
	if err != nil {
		return err
	}

	if p.dependencyUpdate {
		downloadManager := &downloader.Manager{
			Out:       p.out,
			ChartPath: path,
			HelmHome:  settings.Home,
			Keyring:   p.keyring,
			Getters:   getter.All(settings),
			Debug:     settings.Debug,
		}

		if err := downloadManager.Update(); err != nil {
			return err
		}
	}

	ch, err := chartutil.LoadDir(path)
	if err != nil {
		return err
	}

	// If version is set, modify the version.
	if len(p.version) != 0 {
		if err := setVersion(ch, p.version); err != nil {
			return err
		}
		debug("Setting version to %s", p.version)
	}

	if p.appVersion != "" {
		ch.Metadata.AppVersion = p.appVersion
		debug("Setting appVersion to %s", p.appVersion)
	}

	if filepath.Base(path) != ch.Metadata.Name {
		return fmt.Errorf("directory name (%s) and Chart.yaml name (%s) must match", filepath.Base(path), ch.Metadata.Name)
	}

	if reqs, err := chartutil.LoadRequirements(ch); err == nil {
		if err := renderutil.CheckDependencies(ch, reqs); err != nil {
			return err
		}
	} else {
		if err != chartutil.ErrRequirementsNotFound {
			return err
		}
	}

	var dest string
	if p.destination == "." {
		// Save to the current working directory.
		dest, err = os.Getwd()
		if err != nil {
			return err
		}
	} else {
		// Otherwise save to set destination
		dest = p.destination
	}

	name, err := chartutil.Save(ch, dest)
	if err == nil {
		fmt.Fprintf(p.out, "Successfully packaged chart and saved it to: %s\n", name)
	} else {
		return fmt.Errorf("Failed to save: %s", err)
	}

	// Save to $HELM_HOME/local directory. This is second, because we don't want
	// the case where we saved here, but didn't save to the default destination.
	if p.save {
		lr := p.home.LocalRepository()
		if err := repo.AddChartToLocalRepo(ch, lr); err != nil {
			return err
		}
		debug("Successfully saved %s to %s\n", name, lr)
	}

	if p.sign {
		err = p.clearsign(name)
	}

	return err
}

func setVersion(ch *chart.Chart, ver string) error {
	// Verify that version is a SemVer, and error out if it is not.
	if _, err := semver.NewVersion(ver); err != nil {
		return err
	}

	// Set the version field on the chart.
	ch.Metadata.Version = ver
	return nil
}

func (p *packageCmd) clearsign(filename string) error {
	// Load keyring
	signer, err := provenance.NewFromKeyring(p.keyring, p.key)
	if err != nil {
		return err
	}

	if err := signer.DecryptKey(passphraseFetcher); err != nil {
		return err
	}

	sig, err := signer.ClearSign(filename)
	if err != nil {
		return err
	}

	debug(sig)

	return ioutil.WriteFile(filename+".prov", []byte(sig), 0755)
}

// passphraseFetcher implements provenance.PassphraseFetcher
func passphraseFetcher(name string) ([]byte, error) {
	var passphrase = settings.HelmKeyPassphrase()
	if passphrase != "" {
		return []byte(passphrase), nil
	}

	fmt.Printf("Password for key %q >  ", name)
	pw, err := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	return pw, err
}
