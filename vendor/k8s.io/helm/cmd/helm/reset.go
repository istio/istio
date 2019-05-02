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
	"os"

	"github.com/spf13/cobra"

	"k8s.io/client-go/kubernetes"

	"k8s.io/helm/cmd/helm/installer"
	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/proto/hapi/release"
)

const resetDesc = `
This command uninstalls Tiller (the Helm server-side component) from your
Kubernetes Cluster and optionally deletes local configuration in
$HELM_HOME (default ~/.helm/)
`

type resetCmd struct {
	force          bool
	removeHelmHome bool
	namespace      string
	out            io.Writer
	home           helmpath.Home
	client         helm.Interface
	kubeClient     kubernetes.Interface
}

func newResetCmd(client helm.Interface, out io.Writer) *cobra.Command {
	d := &resetCmd{
		out:    out,
		client: client,
	}

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "uninstalls Tiller from a cluster",
		Long:  resetDesc,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := setupConnection(); !d.force && err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("This command does not accept arguments")
			}

			d.namespace = settings.TillerNamespace
			d.home = settings.Home
			d.client = ensureHelmClient(d.client)

			return d.run()
		},
	}

	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.BoolVarP(&d.force, "force", "f", false, "forces Tiller uninstall even if there are releases installed, or if Tiller is not in ready state. Releases are not deleted.)")
	f.BoolVar(&d.removeHelmHome, "remove-helm-home", false, "if set deletes $HELM_HOME")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

// runReset uninstalls tiller from Kubernetes Cluster and deletes local config
func (d *resetCmd) run() error {
	if d.kubeClient == nil {
		_, c, err := getKubeClient(settings.KubeContext, settings.KubeConfig)
		if err != nil {
			return fmt.Errorf("could not get kubernetes client: %s", err)
		}
		d.kubeClient = c
	}

	res, err := d.client.ListReleases(
		helm.ReleaseListStatuses([]release.Status_Code{release.Status_DEPLOYED}),
	)
	if !d.force && err != nil {
		return prettyError(err)
	}

	if !d.force && res != nil && len(res.Releases) > 0 {
		return fmt.Errorf("there are still %d deployed releases (Tip: use --force to remove Tiller. Releases will not be deleted.)", len(res.Releases))
	}

	if err := installer.Uninstall(d.kubeClient, &installer.Options{Namespace: d.namespace}); err != nil {
		return fmt.Errorf("error unstalling Tiller: %s", err)
	}

	if d.removeHelmHome {
		if err := deleteDirectories(d.home, d.out); err != nil {
			return err
		}
	}

	fmt.Fprintln(d.out, "Tiller (the Helm server-side component) has been uninstalled from your Kubernetes Cluster.")
	return nil
}

// deleteDirectories deletes $HELM_HOME
func deleteDirectories(home helmpath.Home, out io.Writer) error {
	if _, err := os.Stat(home.String()); err == nil {
		fmt.Fprintf(out, "Deleting %s \n", home.String())
		if err := os.RemoveAll(home.String()); err != nil {
			return fmt.Errorf("Could not remove %s: %s", home.String(), err)
		}
	}

	return nil
}
