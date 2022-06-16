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
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"istio.io/pkg/log"
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

var (
	bases   = map[string]v1.Image{}
	basesMu sync.RWMutex
)

func WarmBase(baseImages ...string) {
	basesMu.Lock()
	wg := sync.WaitGroup{}
	wg.Add(len(baseImages))
	resolvedBaseImages := make([]v1.Image, len(baseImages))
	go func() {
		wg.Wait()
		for i, rbi := range resolvedBaseImages {
			bases[baseImages[i]] = rbi
		}
		basesMu.Unlock()
	}()

	t0 := time.Now()
	for i, b := range baseImages {
		b, i := b, i
		go func() {
			ref, err := name.ParseReference(b)
			if err != nil {
				log.WithLabels("image", b).Warnf("base failed: %v", err)
				return
			}
			bi, err := remote.Image(ref, remote.WithProgress(CreateProgress(fmt.Sprintf("base %v", ref))))
			if err != nil {
				log.WithLabels("image", b).Warnf("base failed: %v", err)
				return
			}
			log.WithLabels("image", b, "step", time.Since(t0)).Infof("base loaded")
			resolvedBaseImages[i] = bi
			wg.Done()
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

func Build(b BuildSpec) error {
	t0 := time.Now()
	lt := t0
	trace := func(d ...interface{}) {
		log.WithLabels("image", b.Name, "total", time.Since(t0), "step", time.Since(lt)).Infof(d...)
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
		baseImage := empty.Image
		if args.Base != "" {
			basesMu.RLock()
			baseImage = bases[args.Base]
			basesMu.RUnlock()
		}
		if baseImage == nil {
			log.Warnf("on demand loading base image %q", args.Base)
			ref, err := name.ParseReference(args.Base)
			if err != nil {
				return err
			}
			bi, err := remote.Image(ref, remote.WithProgress(CreateProgress(fmt.Sprintf("base %v", ref))))
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
			return ioutil.NopCloser(bytes.NewReader(buf.Bytes())), nil
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

	var artifact remote.Taggable
	if len(images) == 1 {
		// Single image, just push that
		artifact = images[0]
	} else {
		// Multiple, we need to create an index
		var manifest v1.ImageIndex = empty.Index
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
			os, arch, _ := strings.Cut(b.Args[idx].Arch, "/")
			manifest = mutate.AppendManifests(manifest, mutate.IndexAddendum{
				Add: i,
				Descriptor: v1.Descriptor{
					MediaType: mt,
					Size:      size,
					Digest:    h,
					Platform: &v1.Platform{
						Architecture: arch,
						OS:           os,
						Variant:      "", // TODO?
					},
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

func Experiment() {
	registry.New()
}
