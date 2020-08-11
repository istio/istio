package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"istio.io/istio/cni/pkg/taint"
	"istio.io/pkg/log"
)

type ControllerOptions struct {
	RunAsDaemon  bool           `json:"run_as_daemon"`
	TaintOptions *taint.Options `json:"taint_options"`
}

var (
	loggingOptions = log.DefaultOptions()
)

const (
	LeassLockName      = "istio-taint-lock"
	LeaseLockNamespace = "kube-system"
)

// Parse command line options
func parseFlags() (options *ControllerOptions) {
	// Parse command line flags
	//configmap name Options

	pflag.String("configmap-namespace", "kube-system", "the namespace of critical pod definition configmap")
	pflag.String("configmap-name", "single", "the name of critical pod definition configmap")
	pflag.Bool("run-as-daemon", false, "Controller will run in a loop")
	pflag.Bool("help", false, "Print usage information")

	pflag.Parse()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		log.Fatal("Error parsing command line args: %+v")
	}

	if viper.GetBool("help") {
		pflag.Usage()
		os.Exit(0)
	}

	viper.SetEnvPrefix("TAINT")
	viper.AutomaticEnv()
	// Pull runtime args into structs
	options = &ControllerOptions{
		RunAsDaemon: viper.GetBool("run-as-daemon"),
		TaintOptions: &taint.Options{
			ConfigmapName:      viper.GetString("configmap-name"),
			ConfigmapNamespace: viper.GetString("configmap-namespace"),
		},
	}

	return
}

// Set up Kubernetes client using kubeconfig (or in-cluster config if no file provided)
func clientSetup() (clientset *client.Clientset, err error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return
	}
	clientset, err = client.NewForConfig(config)
	return
}

// Log human-readable output describing the current filter and option selection
func logCurrentOptions(ts *taint.TaintSetter, options *ControllerOptions) {
	if options.RunAsDaemon {
		log.Infof("Controller Option: Running as a Daemon.")
	}
	for _, cs := range ts.GetAllConfigs() {
		log.Infof("ConfigSetting %s", cs.ToString())
	}
}

//check all node, taint all unready node
func nodeReadinessCheck(tc *taint.Controller) {
	nodes := tc.ListAllNode()
	for _, node := range nodes {
		err := tc.ProcessNode(node)
		if err != nil {
			log.Fatalf("error: %+v in node %v", err.Error(), node.Name)
		}
	}
}

func main() {
	loggingOptions.OutputPaths = []string{"stderr"}
	loggingOptions.JSONEncoding = true
	if err := log.Configure(loggingOptions); err != nil {
		os.Exit(1)
	}
	options := parseFlags()

	clientSet, err := clientSetup()
	if err != nil {
		log.Fatalf("Could not construct clientSet: %s", err)
	}
	taintSetter, err := taint.NewTaintSetter(clientSet, options.TaintOptions)
	if err != nil {
		log.Fatalf("Could not construct taint setter: %s", err)
	}
	logCurrentOptions(&taintSetter, options)
	tc, err := taint.NewTaintSetterController(&taintSetter)
	if err != nil {
		log.Fatalf("Fatal error constructing taint controller: %+v", err)
	}
	if options.RunAsDaemon {
		id := uuid.New().String()
		stopCh := make(chan struct{})
		lock := &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      LeassLockName,
				Namespace: LeaseLockNamespace,
			},
			Client: clientSet.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: id,
			},
		}
		leadelectionCallback := leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				//once leader elected it should taint all nodes at first to prevent race condition
				tc.RegistTaints()
				tc.Run(ctx.Done()) //graceful shut down
			},
			OnStoppedLeading: func() {
				// when leader failed, log leader failure and restart leader election
				log.Infof("leader lost: %s", id)
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if identity == id {
					// I just got the lock
					return
				}
				log.Infof("new leader elected: %s", identity)
			},
		}
		func(stopCh <-chan struct{}) {
			for {
				ctx, cancel := context.WithCancel(context.Background())
				ch := make(chan os.Signal, 1)
				signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
				go func() {
					<-ch
					log.Info("Received termination, signaling shutdown")
					cancel()
				}()
				leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
					Lock:            lock,
					ReleaseOnCancel: true,
					LeaseDuration:   60 * time.Second,
					RenewDeadline:   15 * time.Second,
					RetryPeriod:     5 * time.Second,
					Callbacks:       leadelectionCallback,
				})
				select {
				case <-stopCh:
					return
				default:
					cancel()
					log.Errorf("leader election lost due to exception happened")
				}
			}
		}(stopCh)
	} else {
		//check for node readiness in every node
		nodeReadinessCheck(tc)
	}
}
