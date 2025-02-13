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

package builder

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/tracing"
)

type BuildSpec struct {
	// Name is optional, for logging
	Name  string
	Dests []string
	Args  []Args
}

type Args struct {
	// Name is optional, for logging
	Name string

	// Image architecture. Required only when multiple images present
	Arch string

	Env        map[string]string
	Labels     map[string]string
	User       string
	WorkDir    string
	Entrypoint []string
	Cmd        []string

	// Base image to use
	Base string

	// Files contains all files, mapping destination path -> source path
	Files map[string]string
	// FilesBase is the base path for absolute paths in Files
	FilesBase string
}

type baseKey struct {
	arch string
	name string
}

var (
	bases   = map[baseKey]v1.Image{}
	basesMu sync.RWMutex
)

func WarmBase(ctx context.Context, architectures []string, baseImages ...string) {
	_, span := tracing.Start(ctx, "RunCrane")
	defer span.End()
	basesMu.Lock()
	wg := sync.WaitGroup{}
	wg.Add(len(baseImages) * len(architectures))
	resolvedBaseImages := make([]v1.Image, len(baseImages)*len(architectures))
	keys := []baseKey{}
	for _, a := range architectures {
		for _, b := range baseImages {
			keys = append(keys, baseKey{toPlatform(a).Architecture, b})
		}
	}
	go func() {
		wg.Wait()
		for i, rbi := range resolvedBaseImages {
			bases[keys[i]] = rbi
		}
		basesMu.Unlock()
	}()

	t0 := time.Now()
	for i, b := range keys {
		go func() {
			defer wg.Done()
			ref, err := name.ParseReference(b.name)
			if err != nil {
				log.WithLabels("image", b).Warnf("base failed: %v", err)
				return
			}
			plat := v1.Platform{
				Architecture: b.arch,
				OS:           "linux",
			}
			bi, err := remote.Image(ref, remote.WithPlatform(plat), remote.WithProgress(CreateProgress(fmt.Sprintf("base %v", ref))))
			if err != nil {
				log.WithLabels("image", b).Warnf("base failed: %v", err)
				return
			}
			log.WithLabels("image", b, "step", time.Since(t0)).Infof("base loaded")
			resolvedBaseImages[i] = bi
		}()
	}
}

func ByteCount(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

func Build(ctx context.Context, b BuildSpec) error {
	ctx, span := tracing.Start(ctx, "Build")
	defer span.End()
	t0 := time.Now()
	lt := t0
	trace := func(format string, d ...any) {
		log.WithLabels("image", b.Name, "total", time.Since(t0), "step", time.Since(lt)).Infof(format, d...)
		lt = time.Now()
	}
	if len(b.Dests) == 0 {
		return fmt.Errorf("dest required")
	}

	// Over localhost, compression CPU can be the bottleneck. With remotes, compressing usually saves a lot of time.
	compression := gzip.NoCompression
	for _, d := range b.Dests {
		if !strings.HasPrefix(d, "localhost") {
			compression = gzip.BestSpeed
			break
		}
	}

	var images []v1.Image
	for _, args := range b.Args {
		plat := toPlatform(args.Arch)
		baseImage := empty.Image
		if args.Base != "" {
			basesMu.RLock()
			baseImage = bases[baseKey{arch: plat.Architecture, name: args.Base}] // todo per-arch base
			basesMu.RUnlock()
		}
		if baseImage == nil {
			log.Warnf("on demand loading base image %q", args.Base)
			ref, err := name.ParseReference(args.Base)
			if err != nil {
				return err
			}
			bi, err := remote.Image(
				ref,
				remote.WithPlatform(plat),
				remote.WithProgress(CreateProgress(fmt.Sprintf("base %v", ref))),
			)
			if err != nil {
				return err
			}
			baseImage = bi
		}
		trace("create base")

		cfgFile, err := baseImage.ConfigFile()
		if err != nil {
			return err
		}

		trace("base config")

		// Set our platform on the image. This is largely for empty.Image only; others should already have correct default.
		cfgFile = cfgFile.DeepCopy()
		cfgFile.OS = plat.OS
		cfgFile.Architecture = plat.Architecture

		cfg := cfgFile.Config
		for k, v := range args.Env {
			cfg.Env = append(cfg.Env, fmt.Sprintf("%v=%v", k, v))
		}
		if args.User != "" {
			cfg.User = args.User
		}
		if len(args.Entrypoint) > 0 {
			cfg.Entrypoint = args.Entrypoint
			cfg.Cmd = nil
		}
		if len(args.Cmd) > 0 {
			cfg.Cmd = args.Cmd
			cfg.Entrypoint = nil
		}
		if args.WorkDir != "" {
			cfg.WorkingDir = args.WorkDir
		}
		if len(args.Labels) > 0 && cfg.Labels == nil {
			cfg.Labels = map[string]string{}
		}
		for k, v := range args.Labels {
			cfg.Labels[k] = v
		}

		updated, err := mutate.Config(baseImage, cfg)
		if err != nil {
			return err
		}
		trace("config")

		// Pre-allocated 100mb
		// TODO: cache the size of images, use exactish size
		buf := bytes.NewBuffer(make([]byte, 0, 100*1024*1024))
		if err := WriteArchiveFromFiles(args.FilesBase, args.Files, buf); err != nil {
			return err
		}
		sz := ByteCount(int64(buf.Len()))

		l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
		}, tarball.WithCompressionLevel(compression))
		if err != nil {
			return err
		}
		trace("read layer of size %v", sz)

		image, err := mutate.AppendLayers(updated, l)
		if err != nil {
			return err
		}

		trace("layer")
		images = append(images, image)
	}

	// Write Remote

	err := writeImage(ctx, b, images, trace)
	if err != nil {
		return err
	}

	return nil
}

func writeImage(ctx context.Context, b BuildSpec, images []v1.Image, trace func(format string, d ...any)) error {
	_, span := tracing.Start(ctx, "Write")
	defer span.End()
	var artifact remote.Taggable
	if len(images) == 1 {
		// Single image, just push that
		artifact = images[0]
	} else {
		// Multiple, we need to create an index
		var manifest v1.ImageIndex = empty.Index
		manifest = mutate.IndexMediaType(manifest, types.DockerManifestList)
		for idx, i := range images {
			img := i
			mt, err := img.MediaType()
			if err != nil {
				return fmt.Errorf("failed to get mediatype: %w", err)
			}

			h, err := img.Digest()
			if err != nil {
				return fmt.Errorf("failed to compute digest: %w", err)
			}

			size, err := img.Size()
			if err != nil {
				return fmt.Errorf("failed to compute size: %w", err)
			}
			plat := toPlatform(b.Args[idx].Arch)
			manifest = mutate.AppendManifests(manifest, mutate.IndexAddendum{
				Add: i,
				Descriptor: v1.Descriptor{
					MediaType: mt,
					Size:      size,
					Digest:    h,
					Platform:  &plat,
				},
			})
		}
		artifact = manifest
	}

	// MultiWrite takes a Reference -> Taggable, but won't handle writing to multiple Repos. So we keep
	// a map of Repository -> MultiWrite args.
	remoteTargets := map[name.Repository]map[name.Reference]remote.Taggable{}

	for _, dest := range b.Dests {
		destRef, err := name.ParseReference(dest)
		if err != nil {
			return err
		}
		repo := destRef.Context()
		if remoteTargets[repo] == nil {
			remoteTargets[repo] = map[name.Reference]remote.Taggable{}
		}
		remoteTargets[repo][destRef] = artifact
	}

	for repo, mw := range remoteTargets {
		prog := CreateProgress(fmt.Sprintf("upload %v", repo.String()))
		if err := remote.MultiWrite(mw, remote.WithProgress(prog), remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
			return err
		}
		s := repo.String()
		if len(mw) == 1 {
			for tag := range mw {
				s = tag.String()
			}
		}
		trace("upload %v", s)
	}
	return nil
}

func toPlatform(archString string) v1.Platform {
	os, arch, _ := strings.Cut(archString, "/")
	return v1.Platform{
		Architecture: arch,
		OS:           os,
		Variant:      "", // TODO?
	}
}

func CreateProgress(name string) chan v1.Update {
	updates := make(chan v1.Update, 1000)
	go func() {
		lastLog := time.Time{}
		for u := range updates {
			if time.Since(lastLog) < time.Second && !(u.Total == u.Complete) {
				// Limit to 1 log per-image per-second, unless it is the final log
				continue
			}
			if u.Total == 0 {
				continue
			}
			lastLog = time.Now()
			log.WithLabels("action", name).Infof("progress %s/%s", ByteCount(u.Complete), ByteCount(u.Total))
		}
	}()
	return updates
}
