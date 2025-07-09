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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/image"
	"istio.io/istio/pkg/util/sets"
)

const (
	WindowsBuilderName = "windows-builder"
	LinuxBuilderName   = "container-builder"
)

// RunDocker builds docker images using the `docker buildx bake` commands. Buildx is the
// next-generation docker builder, and `bake` is an important part of that which allows us to
// construct a big build plan which docker can execute in parallel. This provides order of magnitude
// improves over a naive `docker build` flow when building many images.
//
// As an optimization, inputs to the Dockerfile are not built in the container. Rather, we build them
// all outside and copy them into a staging folder that represents all the dependencies for the
// Dockerfile. For example, we first build 'foo' to 'out/linux_amd64/foo'. Then, we copy foo to
// 'out/linux_amd64/dockerx_build/build.docker.foo', along with all other dependencies for the foo
// Dockerfile (as declared in docker.yaml). Creating this staging folder ensures that we do not copy
// the entire source tree into the docker context (expensive) or need complex .dockerignore files.
//
// Once we have these staged folders, we just construct a docker bakefile and pass it to `buildx
// bake` and let it do its work.
func RunDocker(args Args) error {
	requiresSplitBuild := len(args.Architectures) > 1 && (args.Save || !args.Push)
	if !requiresSplitBuild {
		log.Infof("building for architectures: %v", args.Architectures)
		return runDocker(args)
	}
	// https://github.com/docker/buildx/issues/59 - load and save are not supported for multi-arch
	// To workaround, we do a build per-arch, and suffix with the architecture
	for _, arch := range args.Architectures {
		args.Architectures = []string{arch}
		args.suffix = ""
		if arch != "linux/amd64" {
			// For backwards compatibility, we do not suffix linux/amd64
			_, arch, _ := strings.Cut(arch, "/")
			args.suffix = "-" + arch
		}
		log.Infof("building for arch %v", arch)
		if err := runDocker(args); err != nil {
			return err
		}
	}
	return nil
}

func runDocker(args Args) error {
	tarFiles, err := ConstructBakeFiles(args)
	if err != nil {
		return err
	}

	makeStart := time.Now()
	for _, arch := range args.Architectures {
		log.Infof("Running make for %v", args.PlanFor(arch).Targets())
		if err := RunMake(context.Background(), args, arch, args.PlanFor(arch).Targets()...); err != nil {
			return err
		}
	}
	if err := CopyInputs(args); err != nil {
		return err
	}
	log.WithLabels("runtime", time.Since(makeStart)).Infof("make complete")

	dockerStart := time.Now()
	if err := RunBake(args); err != nil {
		return err
	}
	if err := RunSave(args, tarFiles); err != nil {
		return err
	}
	log.WithLabels("runtime", time.Since(dockerStart)).Infof("images complete")
	return nil
}

func CopyInputs(a Args) error {
	for _, target := range a.Targets {
		for _, arch := range a.Architectures {
			bp := a.PlanFor(arch).Find(target)
			if bp == nil {
				continue
			}
			args := bp.Dependencies()
			args = append(args, filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("build.docker.%s", target)))
			if err := RunCommand(a, "tools/docker-copy.sh", args...); err != nil {
				return fmt.Errorf("copy: %v", err)
			}
		}
	}
	return nil
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
			if err := Copy(filepath.Join(root, name+".tar.gz"), filepath.Join(root, alias+".tar.gz")); err != nil {
				return err
			}
		}
	}

	return nil
}

func RunBake(args Args) error {
	targetOsSet := sets.New[string]()
	for _, arch := range args.Architectures {
		os := strings.Split(arch, "/")[0]
		targetOsSet.Insert(os)
	}
	for targetOs := range targetOsSet {
		out := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("docker-bake-%s.json", targetOs))
		_ = os.MkdirAll(filepath.Join(testenv.LocalOut, "release", "docker"), 0o755)
		builderName, err := createBuildxBuilderIfNeeded(args, targetOs)
		if err != nil {
			return err
		}
		c := VerboseCommand("docker", "buildx", "bake", "--builder", builderName, "-f", out, "all")
		c.Stdout = os.Stdout
		err = c.Run()
		if err != nil {
			return err
		}
	}
	return nil
}

// --save requires a custom builder. Automagically create it if needed.
func createBuildxBuilderIfNeeded(a Args, targetOs string) (string, error) {
	log.Infof("Checking builder for architectures: %v, save: %v", a.Architectures, a.Save)
	// If we're also compiling for Windows, we need a special builder.
	if targetOs == "windows" {
		log.Infof("Creating windows builder for %v", targetOs)
		return WindowsBuilderName, exec.Command("sh", "-c", strings.ReplaceAll(`
export DOCKER_CLI_EXPERIMENTAL=enabled
if ! docker buildx ls | grep -q {{builder-name}}; then
  docker buildx create --name {{builder-name}} --platform=windows/amd64
  # Pre-warm the builder. If it fails, fetch logs, but continue
  docker buildx inspect --bootstrap {{builder-name}} || docker logs buildx_buildkit_{{builder-name}}0 || true
fi`, "{{builder-name}}", WindowsBuilderName)).Run()
	}
	if !a.Save {
		return "default", nil // default builder supports all but .save
	}
	if _, f := os.LookupEnv("CI"); !f {
		// If we are not running in CI and the user is not using --save, assume the current
		// builder is OK.
		if !a.Save {
			return "", nil
		}
		// --save is specified so verify if the current builder's driver is `docker-container` (needed to satisfy the export)
		// This is typically used when running release-builder locally.
		// Output an error message telling the user how to create a builder with the correct driver.
		c := VerboseCommand("docker", "buildx", "inspect") // get current builder
		out := new(bytes.Buffer)
		c.Stdout = out
		err := c.Run()
		if err != nil {
			return "", fmt.Errorf("command failed: %v", err)
		}
		matches := regexp.MustCompile(`Driver:\s+(.*)`).FindStringSubmatch(out.String())
		if len(matches) == 0 || matches[1] != "docker-container" {
			return "", fmt.Errorf("the docker buildx builder is not using the docker-container driver needed for .save.\n"+
				"Create a new builder (ex: docker buildx create --driver-opt network=host,image=gcr.io/istio-testing/buildkit:v0.11.0"+
				" --name %s --driver docker-container --buildkitd-flags=\"--debug\" --use)", LinuxBuilderName)
		}
		return LinuxBuilderName, nil
	}
	return LinuxBuilderName, exec.Command("sh", "-c", strings.ReplaceAll(`
export DOCKER_CLI_EXPERIMENTAL=enabled
if ! docker buildx ls | grep -q {{builder-name}}; then
  docker buildx create --driver-opt network=host,image=gcr.io/istio-testing/buildkit:v0.11.0 --name {{builder-name}} --buildkitd-flags="--debug"
  # Pre-warm the builder. If it fails, fetch logs, but continue
  docker buildx inspect --bootstrap {{builder-name}} || docker logs buildx_buildkit_{{builder-name}}0 || true
fi`, "{{builder-name}}", LinuxBuilderName)).Run()
}

// ConstructBakeFiles constructs json files to be passed to `docker buildx bake`.
// This command is an extremely powerful command to build many images in parallel, but is pretty undocumented.
// Most info can be found from the source at https://github.com/docker/buildx/blob/master/bake/bake.go.
// At the moment, as `buildx bake` doesn't support multi-arch builds, we generate one separate bake
// file per architecture, and each bake file can be processed by a different `docker buildx` builder.
func ConstructBakeFiles(a Args) (map[string]string, error) {
	variants := sets.New(a.Variants...)
	// hasDoubleDefault checks if we defined both DefaultVariant and PrimaryVariant. If we did, these
	// are the same exact docker build, just requesting different tags. As an optimization, and to ensure
	// byte-for-byte identical images, we will collapse these into a single build with multiple tags.
	hasDoubleDefault := variants.Contains(DefaultVariant) && variants.Contains(PrimaryVariant)

	// Tar files builds a mapping of tar file name (when used with --save) -> alias for that
	// If the value is "", the tar file exists but has no aliases
	tarFiles := map[string]string{}

	allDestinations := sets.New[string]()
	targets := map[string]Target{}
	// First we gather all the targets that will be built.
	for _, arch := range a.Architectures {
		for _, variant := range a.Variants {
			for _, target := range a.Targets {
				bp := a.PlanFor(arch).Find(target)
				if bp == nil {
					continue
				}
				if variant == DefaultVariant && hasDoubleDefault {
					// This will be process by the PrimaryVariant, skip it here
					continue
				}

				// These images do not actually use distroless even when specified. So skip to avoid extra building
				if strings.HasPrefix(target, "app_") && variant == DistrolessVariant {
					continue
				}

				name := fmt.Sprintf("%s-%s", target, variant)

				if val, exists := targets[name]; exists {
					val.Platforms = append(val.Platforms, arch)
					targets[name] = val
				} else {
					p := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("build.docker.%s", target))
					t := Target{
						Context:    ptr.Of(p),
						Dockerfile: ptr.Of(filepath.Base(bp.Dockerfile)),
						Args:       createArgs(a, target, variant, ""),
						Platforms:  []string{arch},
					}

					t.Tags = append(t.Tags, extractTags(a, target, variant, hasDoubleDefault)...)
					allDestinations.InsertAll(t.Tags...)

					// See https://docs.docker.com/engine/reference/commandline/buildx_build/#output
					if a.Push {
						t.Outputs = []string{"type=registry"}
					} else if a.Save || strings.HasPrefix(arch, "windows/") {
						// We assume that Windows images are always saved, as they can't be output
						// directly to a local Linux docker.
						n := target
						if variant != "" && variant != DefaultVariant { // For default variant, we do not add it.
							n += "-" + variant
						}

						tarFiles[n+a.suffix] = ""
						if variant == PrimaryVariant && hasDoubleDefault {
							tarFiles[n+a.suffix] = target + a.suffix
						}
						t.Outputs = []string{"type=docker,dest=" + filepath.Join(testenv.LocalOut, "release", "docker", n+a.suffix+".tar")}
					} else {
						t.Outputs = []string{"type=docker"}
					}

					if a.NoCache {
						x := true
						t.NoCache = &x
					}

					targets[name] = t
				}
			}
		}
	}

	// Then we separate the targets into different bake files, one per OS.
	for _, targetOs := range []string{"linux", "windows"} {
		bf := BakeFile{
			Target: map[string]Target{},
			Group:  map[string]Group{},
		}

		for name, t := range targets {
			for _, platform := range t.Platforms {
				if strings.HasPrefix(platform, targetOs+"/") {
					bf.Target[name] = t
				}
			}
		}

		if len(bf.Target) == 0 {
			continue
		}

		// Groups just bundles targets together to make them easier to work with
		groups := map[string]Group{}
		allGroups := sets.New[string]()

		for name := range bf.Target {
			split := strings.Split(name, "-")
			variant := split[len(split)-1]
			allGroups.Insert(variant)
			if g, exists := groups[variant]; !exists {
				groups[variant] = Group{Targets: []string{name}}
			} else {
				groups[variant] = Group{Targets: append(g.Targets, name)}
			}
		}

		bf.Group = groups
		bf.Group["all"] = Group{Targets: sets.SortedList(allGroups)}

		out := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("docker-bake-%s.json", targetOs))
		j, err := json.MarshalIndent(bf, "", "  ")
		if err != nil {
			return nil, err
		}
		_ = os.MkdirAll(filepath.Join(testenv.LocalOut, "dockerx_build"), 0o755)

		if a.NoClobber {
			e := errgroup.Group{}
			for _, i := range sets.SortedList(allDestinations) {
				if strings.HasSuffix(i, ":latest") { // Allow clobbering of latest - don't verify existence
					continue
				}
				e.Go(func() error {
					exists, err := image.Exists(i)
					if err != nil {
						return fmt.Errorf("failed to check image existence: %v", err)
					}
					if exists {
						return fmt.Errorf("image %q already exists", i)
					}
					return nil
				})
			}
			if err := e.Wait(); err != nil {
				return nil, err
			}
		}

		err = os.WriteFile(out, j, 0o644)
		if err != nil {
			return nil, fmt.Errorf("failed to write docker bake file %q: %v", out, err)
		}
	}
	return tarFiles, nil
}

func Copy(srcFile, dstFile string) error {
	log.Debugf("Copy %v -> %v", srcFile, dstFile)
	in, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}

	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}
