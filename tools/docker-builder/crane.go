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
	"context"
	"fmt"
	"path"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/log"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/tracing"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/tools/docker-builder/builder"
	"istio.io/istio/tools/docker-builder/dockerfile"
)

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
func RunCrane(ctx context.Context, a Args) error {
	ctx, span := tracing.Start(ctx, "RunCrane")
	defer span.End()
	g := errgroup.Group{}

	variants := sets.New(a.Variants...)
	// hasDoubleDefault checks if we defined both DefaultVariant and PrimaryVariant. If we did, these
	// are the same exact docker build, just requesting different tags. As an optimization, and to ensure
	// byte-for-byte identical images, we will collapse these into a single build with multiple tags.
	hasDoubleDefault := variants.Contains(DefaultVariant) && variants.Contains(PrimaryVariant)

	// First, construct our build plan. Doing this first allows us to figure out which base images we will need,
	// so we can pull them in the background
	builds := []builder.BuildSpec{}
	bases := sets.New[string]()
	for _, v := range a.Variants {
		for _, t := range a.Targets {
			b := builder.BuildSpec{
				Name:  t,
				Dests: extractTags(a, t, v, hasDoubleDefault),
			}
			for _, arch := range a.Architectures {
				p := a.PlanFor(arch).Find(t)
				if p == nil {
					continue
				}
				df := p.Dockerfile
				dargs := createArgs(a, t, v, arch)
				args, err := dockerfile.Parse(df, dockerfile.WithArgs(dargs), dockerfile.IgnoreRuns())
				if err != nil {
					return fmt.Errorf("parse: %v", err)
				}
				args.Arch = arch
				args.Name = t
				// args.Files provides a mapping from final destination -> docker context source
				// docker context is virtual, so we need to rewrite the "docker context source" to the real path of disk
				plan := a.PlanFor(arch).Find(args.Name)
				if plan == nil {
					continue
				}
				// Plan is a list of real file paths, but we don't have a strong mapping from "docker context source"
				// to "real path on disk". We do have a weak mapping though, by reproducing docker-copy.sh
				for dest, src := range args.Files {
					translated, err := translate(plan.Dependencies(), src)
					if err != nil {
						return err
					}
					args.Files[dest] = translated
				}
				bases.Insert(args.Base)
				b.Args = append(b.Args, args)
			}
			builds = append(builds, b)
		}
	}

	// Warm up our base images while we are building everything. This isn't pulling them -- we actually
	// never pull them -- but it is pulling the config file from the remote registry.
	builder.WarmBase(ctx, a.Architectures, sets.SortedList(bases)...)

	// Build all dependencies
	makeStart := time.Now()
	for _, arch := range a.Architectures {
		if err := RunMake(ctx, a, arch, a.PlanFor(arch).Targets()...); err != nil {
			return err
		}
	}
	log.WithLabels("runtime", time.Since(makeStart)).Infof("make complete")

	// Finally, construct images and push them
	dockerStart := time.Now()
	for _, b := range builds {
		g.Go(func() error {
			if err := builder.Build(ctx, b); err != nil {
				return fmt.Errorf("build %v: %v", b.Name, err)
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

func absPath(p string) string {
	if path.IsAbs(p) {
		return p
	}
	return path.Join(testenv.IstioSrc, p)
}
