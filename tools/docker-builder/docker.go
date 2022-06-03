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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
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
	tarFiles, err := ConstructBakeFile(args)
	if err != nil {
		return err
	}

	makeStart := time.Now()
	if err := RunMake(args, args.Plan.Targets()...); err != nil {
		return err
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
		bp := a.Plan.Find(target)
		args := bp.Dependencies()
		args = append(args, filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("build.docker.%s", target)))
		if err := RunCommand(a, "tools/docker-copy.sh", args...); err != nil {
			return fmt.Errorf("copy: %v", err)
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
		return nil // default builder supports all but .save
	}
	if _, f := os.LookupEnv("CI"); !f {
		// If we are not running in CI and the user is not using --save, assume the current
		// builder is OK.
		if !a.Save {
			return nil
		}
		// --save is specified so verify if the current builder's driver is `docker-container` (needed to satisfy the export)
		// This is typically used when running release-builder locally.
		// Output an error message telling the user how to create a builder with the correct driver.
		c := VerboseCommand("docker", "buildx", "ls") // get current builder
		out := new(bytes.Buffer)
		c.Stdout = out
		err := c.Run()
		if err != nil {
			return fmt.Errorf("command failed: %v", err)
		}
		scanner := bufio.NewScanner(out)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Split(line, " ")[1] == "*" { // This is the default builder
				if strings.Split(line, " ")[3] == "docker-container" { // if using docker-container driver
					return nil // current builder will work for --save
				}
				return fmt.Errorf("the docker buildx builder is not using the docker-container driver needed for .save.\n" +
					"Create a new builder (ex: docker buildx create --driver-opt network=host,image=gcr.io/istio-testing/buildkit:v0.10.3" +
					" --name istio-builder --driver docker-container --buildkitd-flags=\"--debug\" --use)")
			}
		}
	}
	return exec.Command("sh", "-c", `
export DOCKER_CLI_EXPERIMENTAL=enabled
if ! docker buildx ls | grep -q container-builder; then
  docker buildx create --driver-opt network=host,image=gcr.io/istio-testing/buildkit:v0.10.3 --name container-builder --buildkitd-flags="--debug"
  # Pre-warm the builder. If it fails, fetch logs, but continue
  docker buildx inspect --bootstrap container-builder || docker logs buildx_buildkit_container-builder0 || true
fi
docker buildx use container-builder`).Run()
}

// ConstructBakeFile constructs a docker-bake.json to be passed to `docker buildx bake`.
// This command is an extremely powerful command to build many images in parallel, but is pretty undocumented.
// Most info can be found from the source at https://github.com/docker/buildx/blob/master/bake/bake.go.
func ConstructBakeFile(a Args) (map[string]string, error) {
	// Targets defines all images we are actually going to build
	targets := map[string]Target{}
	// Groups just bundles targets together to make them easier to work with
	groups := map[string]Group{}

	variants := sets.New(a.Variants...)
	// hasDoubleDefault checks if we defined both DefaultVariant and PrimaryVariant. If we did, these
	// are the same exact docker build, just requesting different tags. As an optimization, and to ensure
	// byte-for-byte identical images, we will collapse these into a single build with multiple tags.
	hasDoubleDefault := variants.Contains(DefaultVariant) && variants.Contains(PrimaryVariant)

	allGroups := sets.New()
	// Tar files builds a mapping of tar file name (when used with --save) -> alias for that
	// If the value is "", the tar file exists but has no aliases
	tarFiles := map[string]string{}

	allDestinations := sets.New()
	for _, variant := range a.Variants {
		for _, target := range a.Targets {
			bp := a.Plan.Find(target)
			if variant == DefaultVariant && hasDoubleDefault {
				// This will be process by the PrimaryVariant, skip it here
				continue
			}

			// These images do not actually use distroless even when specified. So skip to avoid extra building
			if strings.HasPrefix(target, "app_") && variant == DistrolessVariant {
				continue
			}
			p := filepath.Join(testenv.LocalOut, "dockerx_build", fmt.Sprintf("build.docker.%s", target))
			t := Target{
				Context:    sp(p),
				Dockerfile: sp(filepath.Base(bp.Dockerfile)),
				Args:       createArgs(a, target, variant),
				Platforms:  a.Architectures,
			}

			t.Tags = append(t.Tags, extractTags(a, target, variant, hasDoubleDefault)...)
			allDestinations.InsertAll(t.Tags...)

			// See https://docs.docker.com/engine/reference/commandline/buildx_build/#output
			if a.Push {
				t.Outputs = []string{"type=registry"}
			} else if a.Save {
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

			if a.NoCache {
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

	if a.NoClobber {
		e := errgroup.Group{}
		for _, i := range allDestinations.SortedList() {
			if strings.HasSuffix(i, ":latest") { // Allow clobbering of latest - don't verify existence
				continue
			}
			i := i
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
