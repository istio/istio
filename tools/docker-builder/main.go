package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/utils/env"

	"istio.io/pkg/log"
	testenv "istio.io/istio/pkg/test/env"
)

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
		RunMake(args, "docker.pilot2")
	},
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
