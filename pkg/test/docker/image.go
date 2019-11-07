// Copyright 2019 Istio Authors
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

package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

var (
	imageIDRegex = regexp.MustCompile(`Successfully built ([a-z0-9]+)`)
)

type Image string

func (i Image) String() string {
	return string(i)
}

type entry struct {
	mode    os.FileMode
	content []byte
}

// ImageBuilder is a utility for creating Docker images.
type ImageBuilder struct {
	entries map[string]entry
	tags    []string
	err     error
}

// NewImageBuilder creates a new ImageBuilder instance.
func NewImageBuilder() *ImageBuilder {
	return &ImageBuilder{
		entries: make(map[string]entry),
	}
}

// Copy this builder.
func (b *ImageBuilder) Copy() *ImageBuilder {
	return &ImageBuilder{
		tags:    append([]string{}, b.tags...),
		entries: copyEntries(b.entries),
	}
}

// Add an entry for the given bytes.
func (b *ImageBuilder) Add(entryName string, mode os.FileMode, content []byte) *ImageBuilder {
	if b.err != nil {
		return b
	}

	b.entries[entryName] = entry{
		mode:    mode,
		content: content,
	}
	return b
}

// AddFile adds an entry with the contents of the given file.
func (b *ImageBuilder) AddFile(entryName, path string) *ImageBuilder {
	if b.err != nil {
		return b
	}

	// Get the file mode and permissions.
	info, err := os.Stat(path)
	if err != nil {
		b.err = err
		return b
	}
	mode := info.Mode()

	// Read the content.
	content, err := ioutil.ReadFile(path)
	if err != nil {
		b.err = err
		return b
	}

	// Add a raw entry for the file.
	b.Add(entryName, mode, content)
	return b
}

// AddDir adds an entry with all of the files in the given directory (recursively).
func (b *ImageBuilder) AddDir(entryName, path string) *ImageBuilder {
	if b.err != nil {
		return b
	}

	files, err := ioutil.ReadDir(path)
	if err != nil {
		b.err = err
		return b
	}

	for _, f := range files {
		childEntryName := filepath.Join(entryName, f.Name())
		childPath := filepath.Join(path, f.Name())
		if f.IsDir() {
			// Recurse
			b.AddDir(childEntryName, childPath)
			if b.err != nil {
				return b
			}
		} else {
			b.AddFile(childEntryName, childPath)
			if b.err != nil {
				return b
			}
		}
	}
	return b
}

// Tag adds the given tags to the image.
func (b *ImageBuilder) Tag(tags ...string) *ImageBuilder {
	b.tags = append(b.tags, tags...)
	return b
}

// Build the image and return the ID.
func (b *ImageBuilder) Build(dockerClient *client.Client) (Image, error) {
	if b.err != nil {
		return "", b.err
	}

	// Create a tar writer.
	buff := bytes.NewBuffer(nil)
	tw := tar.NewWriter(buff)

	// Add TAR entries for the raw content.
	for entryName, entry := range b.entries {
		if err := writeTarEntry(tw, entryName, entry.mode, entry.content); err != nil {
			return "", err
		}
	}

	// Close the writer and flush to the buffer.
	if err := tw.Close(); err != nil {
		return "", err
	}

	// Build the image.
	resp, err := dockerClient.ImageBuild(context.Background(), buff, types.ImageBuildOptions{
		Tags:        b.tags,
		Remove:      true,
		ForceRemove: true,
		NoCache:     true,
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	matches := imageIDRegex.FindStringSubmatch(string(content))
	if matches == nil {
		return "", fmt.Errorf("failed to find image ID for image build result:\n%s", string(content))
	}

	id := matches[1]

	return Image(id), nil
}

func writeTarEntry(w *tar.Writer, name string, fileMode os.FileMode, content []byte) error {
	if err := w.WriteHeader(&tar.Header{
		Name: name,
		Size: int64(len(content)),
		Mode: int64(fileMode),
	}); err != nil {
		return err
	}
	_, err := w.Write(content)
	return err
}

func copyEntries(in map[string]entry) map[string]entry {
	out := make(map[string]entry)
	for k, v := range in {
		out[k] = v
	}
	return out
}
