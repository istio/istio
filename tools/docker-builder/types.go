package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"k8s.io/utils/env"

	"istio.io/istio/pilot/pkg/util/sets"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/pkg/log"
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
	Push          bool
	Save          bool
	BuildxEnabled bool
	Targets       []string
	Variants      []string
	Architectures []string
	BaseVersion   string
	ProxyVersion  string
	IstioVersion  string
	Tag           string
	Hub           string
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

func DefaultArgs() Args {
	// By default, we build all targets
	targets := []string{
		"pilot",
		"proxyv2",
		"app",
		"istioctl",
		"operator",
		"install-ci",

		"app_sidecar_ubuntu_xenial",
		"app_sidecar_ubuntu_bionic",
		"app_sidecar_ubuntu_focal",
		"app_sidecar_debian_9",
		"app_sidecar_debian_10",
		"app_sidecar_centos_8",
		"app_sidecar_centos_7",
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
	pv, err := testenv.ReadProxySHA()
	if err != nil {
		log.Warnf("failed to read proxy sha")
		pv = "unknown"
	}
	variants := []string{DefaultVariant}
	if legacy, f := os.LookupEnv("DOCKER_BUILD_VARIANTS"); f {
		variants = strings.Split(legacy, " ")
	}

	if os.Getenv("INCLUDE_UNTAGGED_DEFAULT") == "true" {
		// This legacy env var was to workaround the old build logic not being very smart
		// In the new builder, we automagically detect this. So just insert the 'default' variant
		cur := sets.NewSet(variants...)
		cur.Insert(DefaultVariant)
		variants = cur.SortedList()
	}

	arch := []string{"linux/amd64"}
	if legacy, f := os.LookupEnv("DOCKER_ARCHITECTURES"); f {
		arch = strings.Split(legacy, ",")
	}
	return Args{
		Push:          false,
		Save:          false,
		BuildxEnabled: true,
		Hub:           env.GetString("HUB", "localhost:5000"),
		Tag:           env.GetString("TAG", "latest"),
		BaseVersion:   fetchBaseVersion(),
		IstioVersion:  fetchIstioVersion(),
		ProxyVersion:  pv,
		Architectures: arch,
		Targets:       targets,
		Variants:      variants,
	}
}

var (
	args    = DefaultArgs()
	version = false
)

var baseVersionRegexp = regexp.MustCompile(`BASE_VERSION \?= (.*)`)

func fetchBaseVersion() string {
	if b, f := os.LookupEnv("BASE_VERSION"); f {
		return b
	}
	b, err := ioutil.ReadFile(filepath.Join(testenv.IstioSrc, "Makefile.core.mk"))
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
		return "unknown"
	}
	match := baseVersionRegexp.FindSubmatch(b)
	if len(match) < 2 {
		log.Fatalf("failed to find match")
		return "unknown"
	}
	return string(match[1])
}

var istioVersionRegexp = regexp.MustCompile(`VERSION \?= (.*)`)

func fetchIstioVersion() string {
	if b, f := os.LookupEnv("VERSION"); f {
		return b
	}
	b, err := ioutil.ReadFile(filepath.Join(testenv.IstioSrc, "Makefile.core.mk"))
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
		return "unknown"
	}
	match := istioVersionRegexp.FindSubmatch(b)
	if len(match) < 2 {
		log.Fatalf("failed to find match")
		return "unknown"
	}
	return string(match[1])
}
