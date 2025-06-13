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
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/sets"
)

// Types mirrored from https://github.com/docker/buildx/blob/master/bake/bake.go
type Group struct {
	Targets []string `json:"targets" hcl:"targets"`
}

type BakeFile struct {
	Target map[string]Target `json:"target,omitempty"`
	Group  map[string]Group  `json:"group,omitempty"`
}

type Target struct {
	Context          *string           `json:"context,omitempty" hcl:"context,optional"`
	Dockerfile       *string           `json:"dockerfile,omitempty" hcl:"dockerfile,optional"`
	DockerfileInline *string           `json:"dockerfile-inline,omitempty" hcl:"dockerfile-inline,optional"`
	Args             map[string]string `json:"args,omitempty" hcl:"args,optional"`
	Labels           map[string]string `json:"labels,omitempty" hcl:"labels,optional"`
	Tags             []string          `json:"tags,omitempty" hcl:"tags,optional"`
	CacheFrom        []string          `json:"cache-from,omitempty"  hcl:"cache-from,optional"`
	CacheTo          []string          `json:"cache-to,omitempty"  hcl:"cache-to,optional"`
	Target           *string           `json:"target,omitempty" hcl:"target,optional"`
	Secrets          []string          `json:"secret,omitempty" hcl:"secret,optional"`
	SSH              []string          `json:"ssh,omitempty" hcl:"ssh,optional"`
	Platforms        []string          `json:"platforms,omitempty" hcl:"platforms,optional"`
	Outputs          []string          `json:"output,omitempty" hcl:"output,optional"`
	Pull             *bool             `json:"pull,omitempty" hcl:"pull,optional"`
	NoCache          *bool             `json:"no-cache,omitempty" hcl:"no-cache,optional"`
}

type Args struct {
	Push              bool
	Save              bool
	Builder           string
	SupportsEmulation bool
	NoClobber         bool
	NoCache           bool
	Targets           []string
	Variants          []string
	Architectures     []string
	BaseVersion       string
	BaseImageRegistry string
	ProxyVersion      string
	ZtunnelVersion    string
	IstioVersion      string
	Tags              []string
	Hubs              []string
	// Suffix on artifacts, used for multi-arch images where we cannot use manifests
	suffix string

	// Plan describes the build plan, read from file.
	// This is a map of architecture -> plan, as the plan is arch specific.
	Plan map[string]BuildPlan
}

func (a Args) PlanFor(arch string) BuildPlan {
	return a.Plan[arch]
}

func (a Args) String() string {
	var b strings.Builder
	b.WriteString("Push:              " + fmt.Sprint(a.Push) + "\n")
	b.WriteString("Save:              " + fmt.Sprint(a.Save) + "\n")
	b.WriteString("NoClobber:         " + fmt.Sprint(a.NoClobber) + "\n")
	b.WriteString("NoCache:           " + fmt.Sprint(a.NoCache) + "\n")
	b.WriteString("Targets:           " + fmt.Sprint(a.Targets) + "\n")
	b.WriteString("Variants:          " + fmt.Sprint(a.Variants) + "\n")
	b.WriteString("Architectures:     " + fmt.Sprint(a.Architectures) + "\n")
	b.WriteString("BaseVersion:       " + fmt.Sprint(a.BaseVersion) + "\n")
	b.WriteString("BaseImageRegistry: " + fmt.Sprint(a.BaseImageRegistry) + "\n")
	b.WriteString("ProxyVersion:      " + fmt.Sprint(a.ProxyVersion) + "\n")
	b.WriteString("ZtunnelVersion:    " + fmt.Sprint(a.ZtunnelVersion) + "\n")
	b.WriteString("IstioVersion:      " + fmt.Sprint(a.IstioVersion) + "\n")
	b.WriteString("Tags:              " + fmt.Sprint(a.Tags) + "\n")
	b.WriteString("Hubs:              " + fmt.Sprint(a.Hubs) + "\n")
	b.WriteString("Builder:           " + fmt.Sprint(a.Builder) + "\n")
	return b.String()
}

type ImagePlan struct {
	// Name of the image. For example, "pilot"
	Name string `json:"name"`
	// Dockerfile path to build from
	Dockerfile string `json:"dockerfile"`
	// Files list files that are copied as-is into the image
	Files []string `json:"files"`
	// Targets list make targets that are ran and then copied into the image
	Targets []string `json:"targets"`
	// Base indicates if this is a base image or not
	Base bool `json:"base"`
	// EmulationRequired indicates if emulation is required when cross-compiling. It typically is not,
	// as most building in Istio is done outside of docker.
	// When this is set, cross-compile is disabled for components unless emulation is epxlicitly specified
	EmulationRequired bool `json:"emulationRequired"`
	// Platforms for which this image can be built.
	Platforms []string `json:"platforms"`
}

func (p ImagePlan) Dependencies() []string {
	v := []string{p.Dockerfile}
	v = append(v, p.Files...)
	v = append(v, p.Targets...)
	return v
}

type BuildPlan struct {
	Images []ImagePlan `json:"images"`
}

func (p BuildPlan) Targets() []string {
	tgts := sets.New("init") // All targets depend on init
	for _, img := range p.Images {
		tgts.InsertAll(img.Targets...)
	}
	return sets.SortedList(tgts)
}

func (p BuildPlan) Find(n string) *ImagePlan {
	for _, i := range p.Images {
		if i.Name == n {
			return &i
		}
	}
	return nil
}

// Define variants, which control the base image of an image.
// Tags will have the variant append (like 1.0-distroless).
// The DefaultVariant is a special variant that has no explicit tag (like 1.0); it
// is not a unique variant though. Currently, it represents DebugVariant.
// If both DebugVariant and DefaultVariant are built, there will be a single build but multiple tags
const (
	// PrimaryVariant is the variant that DefaultVariant actually builds
	PrimaryVariant = DebugVariant

	DefaultVariant    = "default"
	DebugVariant      = "debug"
	DistrolessVariant = "distroless"
)

const (
	CraneBuilder  = "crane"
	DockerBuilder = "docker"
)

func DefaultArgs() Args {
	// By default, we build all targets
	var targets []string
	_, nonBaseImages, err := ReadPlanTargets()
	if err == nil {
		targets = nonBaseImages
	}
	if legacy, f := os.LookupEnv("DOCKER_TARGETS"); f {
		// Allow env var config. It is a string separated list like "docker.pilot docker.proxy"
		targets = []string{}
		for _, v := range strings.Split(legacy, " ") {
			if v == "" {
				continue
			}
			targets = append(targets, strings.TrimPrefix(v, "docker."))
		}
	}
	pv, err := testenv.ReadDepsSHA("PROXY_REPO_SHA")
	if err != nil {
		log.Warnf("failed to read proxy sha")
		pv = "unknown"
	}
	zv, err := testenv.ReadDepsSHA("ZTUNNEL_REPO_SHA")
	if err != nil {
		log.Warnf("failed to read ztunnel sha")
		zv = "unknown"
	}
	variants := []string{DefaultVariant}
	if legacy, f := os.LookupEnv("DOCKER_BUILD_VARIANTS"); f {
		variants = strings.Split(legacy, " ")
	}

	if os.Getenv("INCLUDE_UNTAGGED_DEFAULT") == "true" {
		// This legacy env var was to workaround the old build logic not being very smart
		// In the new builder, we automagically detect this. So just insert the 'default' variant
		cur := sets.New(variants...)
		cur.Insert(DefaultVariant)
		variants = sets.SortedList(cur)
	}

	arch := []string{"linux/amd64"}
	if legacy, f := os.LookupEnv("DOCKER_ARCHITECTURES"); f {
		arch = strings.Split(legacy, ",")
	}

	hub := []string{env.Register("HUB", "localhost:5000", "").Get()}
	if hubs, f := os.LookupEnv("HUBS"); f {
		hub = strings.Split(hubs, " ")
	}
	tag := []string{env.Register("TAG", "latest", "").Get()}
	if tags, f := os.LookupEnv("TAGS"); f {
		tag = strings.Split(tags, " ")
	}

	builder := DockerBuilder
	if b, f := os.LookupEnv("ISTIO_DOCKER_BUILDER"); f {
		builder = b
	}

	// TODO: consider automatically detecting Qemu support
	qemu := false
	if b, f := os.LookupEnv("ISTIO_DOCKER_QEMU"); f {
		q, err := strconv.ParseBool(b)
		if err == nil {
			qemu = q
		}
	}

	return Args{
		Push:              false,
		Save:              false,
		NoCache:           false,
		Hubs:              hub,
		Tags:              tag,
		BaseVersion:       fetchBaseVersion(),
		BaseImageRegistry: fetchIstioBaseReg(),
		IstioVersion:      fetchIstioVersion(),
		ProxyVersion:      pv,
		ZtunnelVersion:    zv,
		Architectures:     arch,
		Targets:           targets,
		Variants:          variants,
		Builder:           builder,
		SupportsEmulation: qemu,
	}
}

var (
	globalArgs = DefaultArgs()
	version    = false
)

var baseVersionRegexp = regexp.MustCompile(`BASE_VERSION \?= (.*)`)

func fetchBaseVersion() string {
	if b, f := os.LookupEnv("BASE_VERSION"); f {
		return b
	}
	b, err := os.ReadFile(filepath.Join(testenv.IstioSrc, "Makefile.core.mk"))
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
		return "unknown"
	}
	match := baseVersionRegexp.FindSubmatch(b)
	if len(match) < 2 {
		log.Fatal("failed to find match")
		return "unknown"
	}
	return string(match[1])
}

var istioVersionRegexp = regexp.MustCompile(`VERSION \?= (.*)`)

func fetchIstioVersion() string {
	if b, f := os.LookupEnv("VERSION"); f {
		return b
	}
	b, err := os.ReadFile(filepath.Join(testenv.IstioSrc, "Makefile.core.mk"))
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
		return "unknown"
	}
	match := istioVersionRegexp.FindSubmatch(b)
	if len(match) < 2 {
		log.Fatal("failed to find match")
		return "unknown"
	}
	return string(match[1])
}

var istioBaseRegRegexp = regexp.MustCompile(`ISTIO_BASE_REGISTRY \?= (.*)`)

func fetchIstioBaseReg() string {
	if b, f := os.LookupEnv("ISTIO_BASE_REGISTRY"); f {
		return b
	}
	b, err := os.ReadFile(filepath.Join(testenv.IstioSrc, "Makefile.core.mk"))
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
		return "unknown"
	}
	match := istioBaseRegRegexp.FindSubmatch(b)
	if len(match) < 2 {
		log.Fatal("failed to find match")
		return "unknown"
	}
	return string(match[1])
}
