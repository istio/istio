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

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
	pkgversion "istio.io/pkg/version"
)

func main() {
	rootCmd.Flags().StringSliceVar(&globalArgs.Hubs, "hub", globalArgs.Hubs, "docker hub(s)")
	rootCmd.Flags().StringSliceVar(&globalArgs.Tags, "tag", globalArgs.Tags, "docker tag(s)")

	rootCmd.Flags().StringVar(&globalArgs.BaseVersion, "base-version", globalArgs.BaseVersion, "base version to use")
	rootCmd.Flags().StringVar(&globalArgs.ProxyVersion, "proxy-version", globalArgs.ProxyVersion, "proxy version to use")
	rootCmd.Flags().StringVar(&globalArgs.IstioVersion, "istio-version", globalArgs.IstioVersion, "istio version to use")

	rootCmd.Flags().StringSliceVar(&globalArgs.Targets, "targets", globalArgs.Targets, "targets to build")
	rootCmd.Flags().StringSliceVar(&globalArgs.Variants, "variants", globalArgs.Variants, "variants to build")
	rootCmd.Flags().StringSliceVar(&globalArgs.Architectures, "architecures", globalArgs.Architectures, "architectures to build")
	rootCmd.Flags().BoolVar(&globalArgs.Push, "push", globalArgs.Push, "push targets to registry")
	rootCmd.Flags().BoolVar(&globalArgs.Save, "save", globalArgs.Save, "save targets to tar.gz")
	rootCmd.Flags().BoolVar(&globalArgs.NoCache, "no-cache", globalArgs.NoCache, "disable caching")
	rootCmd.Flags().BoolVar(&globalArgs.NoClobber, "no-clobber", globalArgs.NoClobber, "do not allow pushing images that already exist")
	rootCmd.Flags().StringVar(&globalArgs.Builder, "builder", globalArgs.Builder, "type of builder to use. options are crane or docker")
	rootCmd.Flags().BoolVar(&version, "version", version, "show build version")

	rootCmd.Flags().BoolVar(&globalArgs.KindLoad, "kind-load", globalArgs.KindLoad, "kind cluster to load into")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

var privilegedHubs = sets.New(
	"docker.io/istio",
	"istio",
	"gcr.io/istio-release",
	"gcr.io/istio-testing",
)

var rootCmd = &cobra.Command{
	SilenceUsage: true,
	Short:        "Builds Istio docker images",
	RunE: func(cmd *cobra.Command, _ []string) error {
		t0 := time.Now()
		defer func() {
			log.WithLabels("runtime", time.Since(t0)).Infof("build complete")
		}()
		if version {
			fmt.Println(pkgversion.Info.GitRevision)
			os.Exit(0)
		}
		log.Infof("Args: %s", globalArgs)
		if err := ValidateArgs(globalArgs); err != nil {
			return err
		}

		args, err := ReadPlan(globalArgs)
		if err != nil {
			return fmt.Errorf("plan: %v", err)
		}

		// The Istio image builder has two building modes - one utilizing docker, and one manually constructing
		// images using the go-containerregistry (crane) libraries.
		// The crane builder is much faster but less tested.
		// Neither builder is doing standard logic; see each builder for details.
		if args.Builder == CraneBuilder {
			return RunCrane(args)
		}

		return RunDocker(args)
	},
}

func ValidateArgs(a Args) error {
	if len(a.Targets) == 0 {
		return fmt.Errorf("no targets specified")
	}
	if a.Push && a.Save {
		// TODO(https://github.com/moby/buildkit/issues/1555) support both
		return fmt.Errorf("--push and --save are mutually exclusive")
	}
	_, inCI := os.LookupEnv("CI")
	if a.Push && len(privilegedHubs.Intersection(sets.New(a.Hubs...))) > 0 && !inCI {
		// Safety check against developer error. If they have a legitimate use case, they can set CI var
		return fmt.Errorf("pushing to official registry only supported in CI")
	}
	if !sets.New(DockerBuilder, CraneBuilder).Contains(a.Builder) {
		return fmt.Errorf("unknown builder %v", a.Builder)
	}

	if a.Builder == CraneBuilder && a.Save {
		return fmt.Errorf("crane builder does not support save")
	}
	if a.Builder == CraneBuilder && a.NoClobber {
		return fmt.Errorf("crane builder does not support no-clobber")
	}
	if a.Builder == CraneBuilder && a.NoCache {
		return fmt.Errorf("crane builder does not support no-cache")
	}
	if a.Builder == CraneBuilder && !a.Push {
		return fmt.Errorf("crane builder only supports pushing")
	}
	return nil
}

func ReadPlanTargets() ([]string, []string, error) {
	by, err := ioutil.ReadFile(filepath.Join(testenv.IstioSrc, "tools", "docker.yaml"))
	if err != nil {
		return nil, nil, err
	}
	plan := BuildPlan{}
	if err := yaml.Unmarshal(by, &plan); err != nil {
		return nil, nil, err
	}
	bases := sets.New()
	nonBases := sets.New()
	for _, i := range plan.Images {
		if i.Base {
			bases.Insert(i.Name)
		} else {
			nonBases.Insert(i.Name)
		}
	}
	return bases.SortedList(), nonBases.SortedList(), nil
}

func ReadPlan(a Args) (Args, error) {
	by, err := ioutil.ReadFile(filepath.Join(testenv.IstioSrc, "tools", "docker.yaml"))
	if err != nil {
		return a, err
	}
	plan := BuildPlan{}
	input := os.Expand(string(by), func(s string) string {
		data := map[string]string{
			"SIDECAR": "envoy",
		}
		if r, f := data[s]; f {
			return r
		}

		// Fallback to env
		return os.Getenv(s)
	})
	if err := yaml.Unmarshal([]byte(input), &plan); err != nil {
		return a, err
	}
	tgt := sets.New(a.Targets...)
	known := sets.New()
	for _, img := range plan.Images {
		known.Insert(img.Name)
	}
	if unknown := tgt.Difference(known).SortedList(); len(unknown) > 0 {
		return a, fmt.Errorf("unknown targets: %v", unknown)
	}
	// Filter down to requested targets
	desiredImages := []ImagePlan{}
	for _, i := range plan.Images {
		if tgt.Contains(i.Name) {
			desiredImages = append(desiredImages, i)
		}
	}
	plan.Images = desiredImages
	a.Plan = plan
	return a, nil
}

// VerboseCommand runs a command, outputting stderr and stdout
func VerboseCommand(name string, arg ...string) *exec.Cmd {
	log.Infof("Running command: %v %v", name, strings.Join(arg, " "))
	cmd := exec.Command(name, arg...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd
}

func StandardEnv(args Args) []string {
	env := os.Environ()
	if len(sets.New(args.Targets...).Delete("proxyv2")) <= 1 {
		// If we are building multiple, it is faster to build all binaries in a single invocation
		// Otherwise, build just the single item. proxyv2 is special since it is always built separately with tag=agent.
		// Ideally we would just always build the targets we need but our Makefile is not that smart
		env = append(env, "BUILD_ALL=false")
	}

	env = append(env,
		// Build should already run in container, having multiple layers of docker causes issues
		"BUILD_WITH_CONTAINER=0",
	)
	return env
}

var SkipMake = os.Getenv("SKIP_MAKE")

// RunMake runs a make command for the repo, with standard environment variables set
func RunMake(args Args, c ...string) error {
	if len(c) == 0 {
		log.Infof("nothing to make")
		return nil
	}
	if SkipMake == "true" {
		return nil
	}
	shortArgs := []string{}
	// Shorten output to avoid a ton of long redundant paths
	for _, cs := range c {
		shortArgs = append(shortArgs, filepath.Base(cs))
	}
	if len(c) == 0 {
		log.Infof("Nothing to make")
		return nil
	}
	log.Infof("Running make: %v", strings.Join(shortArgs, " "))
	cmd := exec.Command("make", c...)
	cmd.Env = StandardEnv(args)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Dir = testenv.IstioSrc
	return cmd.Run()
}

// RunCommand runs a command for the repo, with standard environment variables set
func RunCommand(args Args, c string, cargs ...string) error {
	cmd := VerboseCommand(c, cargs...)
	cmd.Env = StandardEnv(args)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Dir = testenv.IstioSrc
	return cmd.Run()
}
