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

	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/proto/hapi/release"
)

const releaseTestDesc = `
The test command runs the tests for a release.

The argument this command takes is the name of a deployed release.
The tests to be run are defined in the chart that was installed.
`

type releaseTestCmd struct {
	name     string
	out      io.Writer
	client   helm.Interface
	timeout  int64
	cleanup  bool
	parallel bool
}

func newReleaseTestCmd(c helm.Interface, out io.Writer) *cobra.Command {
	rlsTest := &releaseTestCmd{
		out:    out,
		client: c,
	}

	cmd := &cobra.Command{
		Use:     "test [RELEASE]",
		Short:   "test a release",
		Long:    releaseTestDesc,
		PreRunE: func(_ *cobra.Command, _ []string) error { return setupConnection() },
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkArgsLength(len(args), "release name"); err != nil {
				return err
			}

			rlsTest.name = args[0]
			rlsTest.client = ensureHelmClient(rlsTest.client)
			return rlsTest.run()
		},
	}

	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.Int64Var(&rlsTest.timeout, "timeout", 300, "time in seconds to wait for any individual Kubernetes operation (like Jobs for hooks)")
	f.BoolVar(&rlsTest.cleanup, "cleanup", false, "delete test pods upon completion")
	f.BoolVar(&rlsTest.parallel, "parallel", false, "run test pods in parallel")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

func (t *releaseTestCmd) run() (err error) {
	c, errc := t.client.RunReleaseTest(
		t.name,
		helm.ReleaseTestTimeout(t.timeout),
		helm.ReleaseTestCleanup(t.cleanup),
		helm.ReleaseTestParallel(t.parallel),
	)
	testErr := &testErr{}

	for {
		select {
		case err := <-errc:
			if prettyError(err) == nil && testErr.failed > 0 {
				return testErr.Error()
			}
			return prettyError(err)
		case res, ok := <-c:
			if !ok {
				break
			}

			if res.Status == release.TestRun_FAILURE {
				testErr.failed++
			}

			fmt.Fprintf(t.out, res.Msg+"\n")

		}
	}

}

type testErr struct {
	failed int
}

func (err *testErr) Error() error {
	return fmt.Errorf("%v test(s) failed", err.failed)
}
