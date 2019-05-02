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
	"io"

	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/downloader"
)

const verifyDesc = `
Verify that the given chart has a valid provenance file.

Provenance files provide crytographic verification that a chart has not been
tampered with, and was packaged by a trusted provider.

This command can be used to verify a local chart. Several other commands provide
'--verify' flags that run the same validation. To generate a signed package, use
the 'helm package --sign' command.
`

type verifyCmd struct {
	keyring   string
	chartfile string

	out io.Writer
}

func newVerifyCmd(out io.Writer) *cobra.Command {
	vc := &verifyCmd{out: out}

	cmd := &cobra.Command{
		Use:   "verify [flags] PATH",
		Short: "verify that a chart at the given path has been signed and is valid",
		Long:  verifyDesc,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("a path to a package file is required")
			}
			vc.chartfile = args[0]
			return vc.run()
		},
	}

	f := cmd.Flags()
	f.StringVar(&vc.keyring, "keyring", defaultKeyring(), "keyring containing public keys")

	return cmd
}

func (v *verifyCmd) run() error {
	_, err := downloader.VerifyChart(v.chartfile, v.keyring)
	return err
}
