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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"istio.io/istio/pilot/pkg/util/sets"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/pkg/log"
	pkgversion "istio.io/pkg/version"
)

func main() {
	rootCmd.Flags().StringSliceVar(&args.Hubs, "hub", args.Hubs, "docker hub(s)")
	rootCmd.Flags().StringVar(&args.Tag, "tag", args.Tag, "docker tag")

	rootCmd.Flags().StringVar(&args.BaseVersion, "base-version", args.BaseVersion, "base version to use")
	rootCmd.Flags().StringVar(&args.ProxyVersion, "proxy-version", args.ProxyVersion, "proxy version to use")
	rootCmd.Flags().StringVar(&args.IstioVersion, "istio-version", args.IstioVersion, "istio version to use")

	rootCmd.Flags().StringSliceVar(&args.Targets, "targets", args.Targets, "targets to build")
	rootCmd.Flags().StringSliceVar(&args.Variants, "variants", args.Variants, "variants to build")
	rootCmd.Flags().StringSliceVar(&args.Architectures, "architecures", args.Architectures, "architectures to build")
	rootCmd.Flags().BoolVar(&args.Push, "push", args.Push, "push targets to registry")
	rootCmd.Flags().BoolVar(&args.Save, "save", args.Save, "save targets to tar.gz")
	rootCmd.Flags().BoolVar(&args.NoCache, "no-cache", args.NoCache, "disable caching")
	rootCmd.Flags().BoolVar(&args.BuildxEnabled, "buildx", args.BuildxEnabled, "use buildx for builds")
	rootCmd.Flags().BoolVar(&args.NoClobber, "no-clobber", args.NoClobber, "do not allow pushing images that already exist")
	rootCmd.Flags().BoolVar(&version, "version", version, "show build version")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

var privilegedHubs = sets.NewSet("docker.io/istio", "istio", "gcr.io/istio-release")

var rootCmd = &cobra.Command{
	SilenceUsage: true,
	Short:        "Builds Istio docker images",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if version {
			fmt.Println(pkgversion.Info.GitRevision)
			os.Exit(0)
		}
		log.Infof("Args: %+v", args)
		if len(args.Targets) == 0 {
			return fmt.Errorf("no targets specified")
		}
		if args.Push && args.Save {
			// TODO(https://github.com/moby/buildkit/issues/1555) support both
			return fmt.Errorf("--push and --save are mutually exclusive")
		}
		_, inCI := os.LookupEnv("CI")
		if args.Push && len(privilegedHubs.Intersection(sets.NewSet(args.Hubs...))) > 0 && !inCI {
			// Safety check against developer error. If they have a legitimate use case, they can set CI var
			return fmt.Errorf("pushing to official registry only supported in CI")
		}

		tarFiles, err := ConstructBakeFile(args)
		if err != nil {
			return err
		}
		targets := []string{}
		for _, t := range args.Targets {
			targets = append(targets, fmt.Sprintf("docker.%s", t))
		}
		if err := RunMake(args, targets...); err != nil {
			return err
		}
		if err := RunBake(args); err != nil {
			return err
		}
		if err := RunSave(args, tarFiles); err != nil {
			return err
		}

		return nil
	},
}

func RunBake(args Args) error {
	out := filepath.Join(testenv.LocalOut, "dockerx_build", "docker-bake.json")
	_ = os.MkdirAll(filepath.Join(testenv.LocalOut, "release", "docker"), 0o755)
	if err := createBuildxBuilderIfNeeded(args); err != nil {
		return err
	}
	c := VerboseCommand("docker", "buildx", "bake", "-f", out, "all")
	c.Stdout = os.Stdout
	return c.Run()
}

// --save requires a custom builder. Automagically create it if needed
func createBuildxBuilderIfNeeded(a Args) error {
	if !a.Save {
		return nil
	}
	if _, f := os.LookupEnv("CI"); !f {
		// For now only do this for CI, we do not want to mess with users config. And users rarely use --save
		return nil
	}
	return exec.Command("sh", "-c", `
export DOCKER_CLI_EXPERIMENTAL=enabled
if ! docker buildx ls | grep -q container-builder; then
  docker buildx create --driver-opt network=host,image=gcr.io/istio-testing/buildkit:v0.9.2 --name container-builder --buildkitd-flags="--debug"
  # Pre-warm the builder. If it fails, fetch logs, but continue
  docker buildx inspect --bootstrap container-builder || docker logs buildx_buildkit_container-builder0 || true
fi
docker buildx use container-builder`).Run()
}

// RunSave handles the --save portion. Part of this is done by buildx natively - it will emit .tar
// files. We need tar.gz though, so we have a bit more work to do
func RunSave(a Args, files map[string]string) error {
	if !a.Save {
		return nil
	}

	root := filepath.Join(testenv.LocalOut, "release", "docker")
	for name, alias := range files {
		// Gzip the file
		if err := VerboseCommand("gzip", "--fast", "--force", filepath.Join(root, name+".tar")).Run(); err != nil {
			return err
		}
		// If it has an alias (ie pilot-debug -> pilot), copy it over. Copy after gzip to avoid double compute.
		if alias != "" {
			if err := CopyFile(filepath.Join(root, name+".tar.gz"), filepath.Join(root, alias+".tar.gz")); err != nil {
				return err
			}
		}
	}

	return nil
}

func CopyFile(src, dst string) error {
	log.Infof("Copying %v -> %v", src, dst)
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open file %v to copy: %v", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(path.Join(dst, ".."), 0o750); err != nil {
		return fmt.Errorf("failed to make destination directory %v: %v", dst, err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create file %v to copy to: %v", dst, err)
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("failed to copy %v to %v: %v", src, dst, err)
	}

	return nil
}

func sp(s string) *string {
	return &s
}

// ConstructBakeFile constructs a docker-bake.json to be passed to `docker buildx bake`.
// This command is an extremely powerful command to build many images in parallel, but is pretty undocumented.
// Most info can be found from the source at https://github.com/docker/buildx/blob/master/bake/bake.go.
func ConstructBakeFile(a Args) (map[string]string, error) {
	// Targets defines all images we are actually going to build
	targets := map[string]Target{}
	// Groups just bundles targets together to make them easier to work with
	groups := map[string]Group{}

	variants := sets.NewSet(a.Variants...)
	// hasDoubleDefault checks if we defined both DefaultVariant and PrimaryVariant. If we did, these
	// are the same exact docker build, just requesting different tags. As an optimization, and to ensure
	// byte-for-byte identical images, we will collapse these into a single build with multiple tags.
	hasDoubleDefault := variants.Contains(DefaultVariant) && variants.Contains(PrimaryVariant)

	allGroups := sets.NewSet()
	// Tar files builds a mapping of tar file name (when used with --save) -> alias for that
	// If the value is "", the tar file exists but has no aliases
	tarFiles := map[string]string{}

	allDestinations := sets.NewSet()
	for _, variant := range a.Variants {
		for _, target := range a.Targets {
			if variant == DefaultVariant && hasDoubleDefault {
				// This will be process by the PrimaryVariant, skip it here
				continue
			}

			baseDist := variant
			if baseDist == DefaultVariant {
				baseDist = PrimaryVariant
			}

			// These images do not actually use distroless even when specified. So skip to avoid extra building
			if strings.HasPrefix(target, "app_") && variant == DistrolessVariant {
				continue
			}
			p := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("docker.%s", target))
			t := Target{
				Context:    sp(p),
				Dockerfile: sp(fmt.Sprintf("Dockerfile.%s", target)),
				Args: map[string]string{
					// Base version defines the tag of the base image to use. Typically, set in the Makefile and not overridden.
					"BASE_VERSION": args.BaseVersion,
					// Base distribution picks which variant to build
					"BASE_DISTRIBUTION": baseDist,
					// Additional metadata injected into some images
					"proxy_version":    args.ProxyVersion,
					"istio_version":    args.IstioVersion,
					"VM_IMAGE_NAME":    vmImageName(target),
					"VM_IMAGE_VERSION": vmImageVersion(target),
				},
				Platforms: args.Architectures,
			}

			for _, h := range a.Hubs {
				if variant == DefaultVariant {
					// For default, we have no suffix
					t.Tags = append(t.Tags, fmt.Sprintf("%s/%s:%s", h, target, a.Tag))
				} else {
					// Otherwise, we have a suffix with the variant
					t.Tags = append(t.Tags, fmt.Sprintf("%s/%s:%s-%s", h, target, a.Tag, variant))
					// If we need a default as well, add it as a second tag for the same image to avoid building twice
					if variant == PrimaryVariant && hasDoubleDefault {
						t.Tags = append(t.Tags, fmt.Sprintf("%s/%s:%s", h, target, a.Tag))
					}
				}
			}
			allDestinations.Insert(t.Tags...)

			// See https://docs.docker.com/engine/reference/commandline/buildx_build/#output
			if args.Push {
				t.Outputs = []string{"type=registry"}
			} else if args.Save {
				n := target
				if variant != "" && variant != DefaultVariant { // For default variant, we do not add it.
					n += "-" + variant
				}

				tarFiles[n] = ""
				if variant == PrimaryVariant && hasDoubleDefault {
					tarFiles[n] = target
				}
				t.Outputs = []string{"type=docker,dest=" + filepath.Join(testenv.LocalOut, "release", "docker", n+".tar")}
			} else {
				t.Outputs = []string{"type=docker"}
			}

			if args.NoCache {
				x := true
				t.NoCache = &x
			}

			name := fmt.Sprintf("%s-%s", target, variant)
			targets[name] = t
			tgts := groups[variant].Targets
			tgts = append(tgts, name)
			groups[variant] = Group{tgts}

			allGroups.Insert(variant)
		}
	}
	groups["all"] = Group{allGroups.SortedList()}
	bf := BakeFile{
		Target: targets,
		Group:  groups,
	}
	out := filepath.Join(testenv.LocalOut, "dockerx_build", "docker-bake.json")
	j, err := json.MarshalIndent(bf, "", "  ")
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Join(testenv.LocalOut, "dockerx_build"), 0o755)

	if args.NoClobber {
		e := errgroup.Group{}
		for _, i := range allDestinations.SortedList() {
			e.Go(func() error {
				return assertImageNonExisting(i)
			})
		}
		if err := e.Wait(); err != nil {
			return nil, err
		}
	}

	return tarFiles, os.WriteFile(out, j, 0o644)
}

func assertImageNonExisting(i string) error {
	c := exec.Command("crane", "manifest", i)
	b := &bytes.Buffer{}
	c.Stderr = b
	err := c.Run()
	if err != nil {
		if strings.Contains(b.String(), "MANIFEST_UNKNOWN") {
			return nil
		}
		return fmt.Errorf("failed to check image existence: %v, %v", err, b.String())
	}
	return fmt.Errorf("image %q already exists", i)
}

func vmImageName(target string) string {
	if !strings.HasPrefix(target, "app_sidecar") {
		// Not a VM
		return ""
	}
	if strings.HasPrefix(target, "app_sidecar_base") {
		return strings.Split(target, "_")[3]
	}

	return strings.Split(target, "_")[2]
}

func vmImageVersion(target string) string {
	if !strings.HasPrefix(target, "app_sidecar") {
		// Not a VM
		return ""
	}
	if strings.HasPrefix(target, "app_sidecar_base") {
		return strings.Split(target, "_")[4]
	}

	return strings.Split(target, "_")[3]
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
	if len(sets.NewSet(args.Targets...).Delete("proxyv2")) <= 1 {
		// If we are building multiple, it is faster to build all binaries in a single invocation
		// Otherwise, build just the single item. proxyv2 is special since it is always built separately with tag=agent.
		// Ideally we would just always build the targets we need but our Makefile is not that smart
		env = append(env, "BUILD_ALL=false")
	}

	env = append(env,
		// Build should already run in container, having multiple layers of docker causes issues
		"BUILD_WITH_CONTAINER=0",
		// Overwrite rules for buildx
		"DOCKER_RULE=./tools/docker-copy.sh $^ $(DOCKERX_BUILD_TOP)/$@",
		"RENAME_TEMPLATE=mkdir -p $(DOCKERX_BUILD_TOP)/$@ && cp $(ECHO_DOCKER)/$(VM_OS_DOCKERFILE_TEMPLATE) $(DOCKERX_BUILD_TOP)/$@/Dockerfile$(suffix $@)",
	)
	return env
}

// RunMake runs a make command for the repo, with standard environment variables set
func RunMake(args Args, c ...string) error {
	cmd := VerboseCommand("make", c...)
	cmd.Env = StandardEnv(args)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Dir = testenv.IstioSrc
	return cmd.Run()
}
