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
	"path"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tools/docker-builder/builder"
	"istio.io/istio/tools/docker-builder/dockerfile"
	"istio.io/pkg/log"
)

type craneBuild struct {
	args  builder.Args
	dests []string
}

// RunCrane builds docker images using go-containerregistry, rather than relying on Docker. This
// works by parsing each Dockerfile and determining the resulting image config (labels, entrypoint,
// env vars, etc) as well as all files that should be copied in. Notably, RUN is not supported. This
// is not a problem for Istio, as all of our images do any building outside the docker context
// anyway.
//
// Once we have determined the config, we use the go-containerregistry to apply this config on top of
// the configured base image, and add a new layer for all the copies. This layer is constructed in a
// highly optimized manner - rather than copying things around from original source, to docker
// staging folder, to docker context, to a tar file, etc, we directly read the original source files
// into memory and stream them into an in memory tar buffer.
//
// Building in this way ends up being roughly 10x faster than docker. Future work to enable
// sha256-simd (https://github.com/google/go-containerregistry/issues/1330) makes this even faster -
// pushing all images drops to sub-second times, with the registry being the bottleneck (which could
// also use sha256-simd possibly).
func RunCrane(a Args) error {
	g := errgroup.Group{}

	variants := sets.New(a.Variants...)
	// hasDoubleDefault checks if we defined both DefaultVariant and PrimaryVariant. If we did, these
	// are the same exact docker build, just requesting different tags. As an optimization, and to ensure
	// byte-for-byte identical images, we will collapse these into a single build with multiple tags.
	hasDoubleDefault := variants.Contains(DefaultVariant) && variants.Contains(PrimaryVariant)

	// First, construct our build plan. Doing this first allows us to figure out which base images we will need,
	// so we can pull them in the background
	builds := []craneBuild{}
	bases := sets.New()
	for _, v := range a.Variants {
		for _, t := range a.Targets {
			df := a.Plan.Find(t).Dockerfile
			dargs := createArgs(a, t, v)
			args, err := dockerfile.Parse(df, dockerfile.WithArgs(dargs), dockerfile.IgnoreRuns())
			if err != nil {
				return fmt.Errorf("parse: %v", err)
			}
			args.Name = t
			dests := extractTags(a, t, v, hasDoubleDefault)
			bases.Insert(args.Base)
			builds = append(builds, craneBuild{
				args:  args,
				dests: dests,
			})
		}
	}

	// Warm up our base images while we are building everything. This isn't pulling them -- we actually
	// never pull them -- but it is pulling the config file from the remote registry.
	builder.WarmBase(bases.SortedList()...)

	// Build all dependencies
	makeStart := time.Now()
	if err := RunMake(a, a.Plan.Targets()...); err != nil {
		return err
	}
	log.WithLabels("runtime", time.Since(makeStart)).Infof("make complete")

	// Finally, construct images and push them
	dockerStart := time.Now()
	for _, b := range builds {
		b := b
		// b.args.Files provides a mapping from final destination -> docker context source
		// docker context is virtual, so we need to rewrite the "docker context source" to the real path of disk
		plan := a.Plan.Find(b.args.Name)
		// Plan is a list of real file paths, but we don't have a strong mapping from "docker context source"
		// to "real path on disk". We do have a weak mapping though, by reproducing docker-copy.sh
		for dest, src := range b.args.Files {
			translated, err := translate(plan.Dependencies(), src)
			if err != nil {
				return err
			}
			b.args.Files[dest] = translated
		}
		g.Go(func() error {
			if err := builder.Build(b.args, b.dests); err != nil {
				return fmt.Errorf("build %v: %v", b.args.Name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	log.WithLabels("runtime", time.Since(dockerStart)).Infof("images complete")
	return nil
}

// translate takes a "docker context path" and a list of real paths and finds the real path for the associated
// docker context path. Example: translate([out/linux_amd64/binary, foo/other], amd64/binary) -> out/linux_amd64/binary
func translate(plan []string, src string) (string, error) {
	src = filepath.Clean(src)
	base := filepath.Base(src)

	// TODO: this currently doesn't handle multi-arch
	// Likely docker.yaml should explicitly declare multi-arch targets
	for _, p := range plan {
		pb := filepath.Base(p)
		if pb == base {
			return absPath(p), nil
		}
	}

	// Next check for folder. This should probably be arbitrary depth
	// Example: plan=[certs], src=certs/foo.bar
	dir := filepath.Dir(src)
	for _, p := range plan {
		pb := filepath.Base(p)
		if pb == dir {
			return absPath(filepath.Join(p, base)), nil
		}
	}

	return "", fmt.Errorf("failed to find real source for %v. plan: %+v", src, plan)
}

func sp(s string) *string {
	return &s
}

func absPath(p string) string {
	if path.IsAbs(p) {
		return p
	}
	return path.Join(testenv.IstioSrc, p)
}
