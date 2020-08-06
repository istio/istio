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
	"k8s.io/kubectl/pkg/util/podutils"

	"istio.io/istio/cni/pkg/taint"
	"istio.io/pkg/log"
)

type ControllerOptions struct {
	RunAsDaemon     bool           `json:"run_as_daemon"`
	RegistWithTaint bool           `json:"regist_with_taint"`
	TaintOptions    *taint.Options `json:"taint_options"`
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
	pflag.Bool("run-as-daemon", true, "Controller will run in a loop")
	pflag.Bool("regist-with-taint", true, "all nodes will be tainted at first waiting for critical pods to be ready")
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
		RunAsDaemon:     viper.GetBool("run-as-daemon"),
		RegistWithTaint: viper.GetBool("regist-with-taint"),
		TaintOptions: &taint.Options{
			ConfigName:      viper.GetString("configmap-name"),
			ConfigNameSpace: viper.GetString("configmap-namespace"),
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
func nodeReadinessCheck(taintSetter *taint.TaintSetter) {
	nodes, err := taintSetter.GetAllNodes()
	if err != nil {
		log.Fatalf("Fatal error Listing all nodes. %+v", err)
	}
	for _, node := range nodes.Items {
		err := taintSetter.ProcessNode(&node)
		if err != nil {
			log.Fatalf("error: %+v in node %v", err.Error(), node.Name)
		}
	}
}

//get all critical pods and check for the readiness of pod
//if pod is ready, check all critical pod in the node and if all of them are ready untaint the node,
//otherwise taint it
func podReadinessCheck(taintSetter *taint.TaintSetter) {
	pods, err := taintSetter.ListCandidatePods()
	if err != nil {
		log.Fatalf("Fatal error Listing all critical pods: %+v", err)
	}
	for _, pod := range pods.Items {
		if !podutils.IsPodReady(&pod) {
			err := taintSetter.ProcessReadyPod(&pod)
			if err != nil {
				log.Fatalf("error: %+v in ready pod %+v in namespace %+v", err.Error(), pod.Name, pod.Namespace)
			}
		} else {
			err := taintSetter.ProcessUnReadyPod(&pod)
			if err != nil {
				log.Fatalf("error: %+v in runeady pod %+v in namespace %+v", err.Error(), pod.Name, pod.Namespace)
			}
		}
	}
}
func registTaints(taintSetter taint.TaintSetter) {
	Nodes, err := taintSetter.GetAllNodes()
	if err != nil {
		log.Fatalf("Fatal error cannot get all pods: %+v", err)
	}
	for _, node := range Nodes.Items {
		err := taintSetter.AddReadinessTaint(&node)
		if err != nil {
			log.Fatalf("Fatal error cannot taint node: %+v", err)
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
	if err != nil {
		panic(err.Error())
	}
	if options.RunAsDaemon {
		id := uuid.New().String()
		stopCh := make(chan struct{})
		//it will be run in leader for controller configuration and running
		run := func(ctx context.Context) {
			tc, err := taint.NewTaintSetterController(&taintSetter)
			if err != nil {
				log.Fatalf("Fatal error constructing taint controller: %+v", err)
			}
			tc.Run(stopCh)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// listen for interrupts or the Linux SIGTERM signal and cancel
		// our context, which the leader election code will observe and
		// step down
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-ch
			close(stopCh)
			log.Info("Received termination, signaling shutdown")
			cancel()
		}()
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
		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock:            lock,
			ReleaseOnCancel: true,
			LeaseDuration:   3 * time.Second,
			RenewDeadline:   2 * time.Second,
			RetryPeriod:     1 * time.Second,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					//once leader elected it should taint all nodes at first to prevent race condition
					registTaints(taintSetter)
					run(ctx)
				},
				OnStoppedLeading: func() {
					// when leader failed, re-add taints to all nodes to prevent race condition
					log.Infof("leader lost: %s", id)
					registTaints(taintSetter)
					os.Exit(0)
				},
				OnNewLeader: func(identity string) {
					// we're notified when new leader elected
					if identity == id {
						// I just got the lock
						return
					}
					log.Infof("new leader elected: %s", identity)
				},
			},
		})
	} else {
		//check for node readiness in every node
		nodeReadinessCheck(&taintSetter)
		//check for every critical pods' readiness
		podReadinessCheck(&taintSetter)
	}
}
