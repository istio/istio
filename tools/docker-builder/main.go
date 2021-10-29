package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/utils/env"

	"istio.io/istio/pilot/pkg/util/sets"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/pkg/log"
)

type Group struct {
	Targets []string `json:"targets" hcl:"targets"`
}

type BakeFile struct {
	Target map[string]Target `json:"target,omitempty"`
	Group  map[string]Group `json:"group,omitempty"`
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
	Save          bool
	BuildxEnabled bool
	Targets       []string
	Variants      []string
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
		log.Warnf("Using legacy DOCKER_TARGETS; use --targets instead")
		targets = []string{}
		for _, v := range strings.Split(legacy, " ") {
			targets = append(targets, strings.TrimPrefix(v, "docker."))
		}
	}
	return Args{
		Push:          false,
		Save:          false,
		BuildxEnabled: true,
		Hub:           env.GetString("HUB", "localhost:5000"),
		Tag:           env.GetString("TAG", "latest"),
		Targets:       targets,
		Variants:      []string{"default"},
	}
}

var rootCmd = &cobra.Command{
	Use:   "",
	Short: "Builds Istio docker images",
	Run: func(cmd *cobra.Command, _ []string) {
		log.Infof("Args: %+v", args)
		ConstructBakeFile(args)
		RunMake(args, "docker.pilot2")
	},
}

func sp(s string) *string {
	return &s
}

func ConstructBakeFile(a Args) error {
	targets := map[string]Target{}
	for _, variant := range a.Variants {
		for _, target := range a.Targets {
			// TODO remove 2
			p := filepath.Join(testenv.IstioOut, "dockerx_build", fmt.Sprintf("docker.%s2", target))
			t := Target{
				Context:    sp(p),
				Dockerfile: sp(fmt.Sprintf("Dockerfile.%s", target)),
				Args: map[string]string{
					"BASE_VERSION":      "2021-10-06T19-01-15",
					"BASE_DISTRIBUTION": "debug",
					"proxy_version":     "istio-proxy:74393cf764c167b545f32eb895314e624186e5b6",
					"istio_version":     "1.12-dev",
					"VM_IMAGE_NAME":     "",
					"VM_IMAGE_VERSION":  "",
				},
				Tags:      []string{fmt.Sprintf("%s/%s:%s-%s", a.Hub, target, a.Tag, variant)},
				Platforms: []string{"linux/amd64"},
				Outputs:   []string{"type=docker"}, // TODO push
			}
			targets[fmt.Sprintf("%s-%s", target, variant)] = t
		}
	}
	bf := BakeFile{
		Target: targets,
		Group:  nil,
	}
	out := filepath.Join(testenv.IstioOut, "dockerx_build", "docker-bake.json")
	j, err := json.MarshalIndent(bf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, j, 0o644)
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
	env = append(env,
		"BUILD_WITH_CONTAINER=0", // Build should already run in container, having multiple layers of docker causes issues
	)
	return env
}

// RunMake runs a make command for the repo, with standard environment variables set
func RunMake(args Args, c ...string) error {
	cmd := VerboseCommand("make", c...)
	cmd.Env = StandardEnv(args)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Dir = testenv.IstioSrc
	log.Infof("Running make %v with wd=%v", strings.Join(c, " "), cmd.Dir)
	return cmd.Run()
}

var args = DefaultArgs()

func main() {
	rootCmd.Flags().StringVar(&args.Hub, "hub", args.Hub, "docker hub")
	rootCmd.Flags().StringVar(&args.Tag, "tag", args.Tag, "docker tag")
	rootCmd.Flags().StringSliceVar(&args.Targets, "targets", args.Targets, "targets to build")
	rootCmd.Flags().StringSliceVar(&args.Variants, "variants", args.Variants, "variants to build")
	rootCmd.Flags().BoolVar(&args.Push, "push", args.Push, "push targets to registry")
	rootCmd.Flags().BoolVar(&args.Save, "save", args.Save, "save targets to tar.gz")
	rootCmd.Flags().BoolVar(&args.BuildxEnabled, "buildx", args.BuildxEnabled, "use buildx for builds")
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}
