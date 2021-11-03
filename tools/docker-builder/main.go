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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/utils/env"

	"istio.io/istio/pilot/pkg/util/sets"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/pkg/log"
	pkgversion "istio.io/pkg/version"
)

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

	// IMPORTANT: if you add more fields here, do not forget to update newOverrides and README.
}

// docker-builder
// * --push
// * --legacy
// * --targets
// * --save
// * --

type Args struct {
	Push          bool
	AlwaysImport  bool
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

func DefaultArgs() Args {
	targets := []string{
		"pilot",
		"proxyv2",
		"app",

		"app_sidecar_ubuntu_xenia",
		"app_sidecar_ubuntu_bionic",
		"app_sidecar_ubuntu_focal",

		"app_sidecar_debian_9",
		"app_sidecar_debian_10",

		"app_sidecar_centos_8",
		"app_sidecar_centos_7",

		"istioctl",
		"operator",
		"install-ci",
	}
	if legacy, f := os.LookupEnv("DOCKER_TARGETS"); f {
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

var rootCmd = &cobra.Command{
	Use:   "",
	Short: "Builds Istio docker images",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if version {
			fmt.Println(pkgversion.Info.GitRevision)
			os.Exit(0)
		}
		log.Infof("Args: %+v", args)
		allTags, err := ConstructBakeFile(args)
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
		if err := RunSave(args, allTags); err != nil {
			return err
		}

		return nil
	},
}

func RunBake(a Args) error {
	out := filepath.Join(testenv.IstioOut, "dockerx_build", "docker-bake.json")
	args := []string{"buildx", "bake", "-f", out, "all"}
	c := VerboseCommand("docker", args...)
	c.Stdout = os.Stdout
	return c.Run()
}

func RunSave(a Args, tags sets.Set) error {
	if !a.Save {
		return nil
	}
	root := filepath.Join(testenv.IstioOut, "release", "docker")
	os.MkdirAll(root, 0o755)
	for _, t := range tags.SortedList() {
		hubSplit := strings.Split(t, "/")
		withoutHub := hubSplit[len(hubSplit)-1]
		tagSplit := strings.Split(withoutHub, ":")
		name := tagSplit[0]
		variantSplit := strings.Split(tagSplit[1], "-")
		variant := variantSplit[len(variantSplit)-1]
		if len(variantSplit) == 1 {
			variant = ""
		}
		n := name
		if variant != "" {
			n += "-" + variant
		}
		if err := VerboseCommand("sh", "-c",
			fmt.Sprintf("docker save %s | gzip --fast > %s",
				t,
				filepath.Join(root, n+".tar.gz"))).Run(); err != nil {
			return err
		}
	}
	return nil
}

func sp(s string) *string {
	return &s
}

const (
	PrimaryVariant = DebugVariant

	DefaultVariant    = "default"
	DebugVariant      = "debug"
	DistrolessVariant = "distroless"
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

func ConstructBakeFile(a Args) (sets.Set, error) {
	targets := map[string]Target{}
	groups := map[string]Group{}
	variants := sets.NewSet(a.Variants...)
	hasDoubleDefault := variants.Contains(DefaultVariant) && variants.Contains(PrimaryVariant)
	allGroups := sets.NewSet()
	allTags := sets.NewSet()
	for _, variant := range a.Variants {
		for _, target := range a.Targets {
			if variant == DefaultVariant && hasDoubleDefault {
				// This will be process by the PrimaryVariant for efficiency
				continue
			}

			baseDist := variant
			if baseDist == DefaultVariant {
				baseDist = PrimaryVariant
			}

			// These images do not actually use distroless even when specified. So skip to avoid extra building
			if strings.HasPrefix("app_", target) && variant == DistrolessVariant {
				continue
			}
			p := filepath.Join(testenv.IstioOut, "dockerx_build", fmt.Sprintf("docker.%s", target))
			t := Target{
				Context:    sp(p),
				Dockerfile: sp(fmt.Sprintf("Dockerfile.%s", target)),
				Args: map[string]string{
					"BASE_VERSION":      args.BaseVersion,
					"BASE_DISTRIBUTION": baseDist,
					"proxy_version":     args.ProxyVersion,
					"istio_version":     args.IstioVersion,
					"VM_IMAGE_NAME":     vmImageName(target),
					"VM_IMAGE_VERSION":  vmImageVersion(target),
				},
				Tags:      []string{fmt.Sprintf("%s/%s:%s-%s", a.Hub, target, a.Tag, variant)},
				Platforms: args.Architectures,
			}

			if variant == DefaultVariant {
				t.Tags = []string{fmt.Sprintf("%s/%s:%s", a.Hub, target, a.Tag)}
			} else {
				t.Tags = []string{fmt.Sprintf("%s/%s:%s-%s", a.Hub, target, a.Tag, variant)}
				if variant == PrimaryVariant && hasDoubleDefault {
					t.Tags = append(t.Tags, fmt.Sprintf("%s/%s:%s", a.Hub, target, a.Tag))
				}
			}
			allTags.Insert(t.Tags...)

			if args.Push {
				t.Outputs = []string{"type=registry"}
				if args.AlwaysImport || args.Save {
					t.Outputs = append(t.Outputs, "type=docker")
				}
			} else {
				t.Outputs = []string{"type=docker"}
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
	out := filepath.Join(testenv.IstioOut, "dockerx_build", "docker-bake.json")
	j, err := json.MarshalIndent(bf, "", "  ")
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Join(testenv.IstioOut, "dockerx_build"), 0o755)
	return allTags, os.WriteFile(out, j, 0o644)
}

func vmImageName(target string) string {
	if !strings.HasPrefix(target, "app_sidecar") {
		// Not a VM
		return ""
	}
	return strings.Split(target, "_")[2]
}

func vmImageVersion(target string) string {
	if !strings.HasPrefix(target, "app_sidecar") {
		// Not a VM
		return ""
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

	// Build should already run in container, having multiple layers of docker causes issues
	env = append(env, "BUILD_WITH_CONTAINER=0")

	// Overwrite rules for buildx
	env = append(env, "DOCKER_RULE=./tools/docker-copy.sh $^ $(DOCKERX_BUILD_TOP)/$@")
	env = append(env, "RENAME_TEMPLATE=mkdir -p $(DOCKERX_BUILD_TOP)/$@ && cp $(ECHO_DOCKER)/$(VM_OS_DOCKERFILE_TEMPLATE) $(DOCKERX_BUILD_TOP)/$@/Dockerfile$(suffix $@)")

	// Hack to make sure we don't hit recusrtion
	env = append(env, "DOCKER_V2_BUILDER=false")
	return env
}

// RunMake runs a make command for the repo, with standard environment variables set
func RunMake(args Args, c ...string) error {
	cmd := VerboseCommand("make", c...)
	cmd.Env = StandardEnv(args)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Dir = testenv.IstioSrc
	log.Infof("Running make %v", strings.Join(c, " "))
	return cmd.Run()
}

var (
	args    = DefaultArgs()
	version = false
)

func main() {
	rootCmd.Flags().StringVar(&args.Hub, "hub", args.Hub, "docker hub")
	rootCmd.Flags().StringVar(&args.Tag, "tag", args.Tag, "docker tag")

	rootCmd.Flags().StringVar(&args.BaseVersion, "base-version", args.BaseVersion, "base version to use")
	rootCmd.Flags().StringVar(&args.ProxyVersion, "proxy-version", args.ProxyVersion, "proxy version to use")
	rootCmd.Flags().StringVar(&args.IstioVersion, "istio-version", args.IstioVersion, "istio version to use")

	rootCmd.Flags().StringSliceVar(&args.Targets, "targets", args.Targets, "targets to build")
	rootCmd.Flags().StringSliceVar(&args.Variants, "variants", args.Variants, "variants to build")
	rootCmd.Flags().StringSliceVar(&args.Architectures, "architecures", args.Architectures, "architectures to build")
	rootCmd.Flags().BoolVar(&args.Push, "push", args.Push, "push targets to registry")
	rootCmd.Flags().BoolVar(&args.AlwaysImport, "import", args.AlwaysImport, "import to local context, even if pushing")
	rootCmd.Flags().BoolVar(&args.Save, "save", args.Save, "save targets to tar.gz")
	rootCmd.Flags().BoolVar(&args.BuildxEnabled, "buildx", args.BuildxEnabled, "use buildx for builds")
	rootCmd.Flags().BoolVar(&version, "version", version, "show build version")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
