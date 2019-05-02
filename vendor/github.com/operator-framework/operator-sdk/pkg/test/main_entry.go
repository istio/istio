// Copyright 2018 The Operator-SDK Authors
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

package test

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/operator-framework/operator-sdk/internal/pkg/scaffold"
	"github.com/operator-framework/operator-sdk/internal/util/projutil"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	log "github.com/sirupsen/logrus"
)

const (
	ProjRootFlag          = "root"
	KubeConfigFlag        = "kubeconfig"
	NamespacedManPathFlag = "namespacedMan"
	GlobalManPathFlag     = "globalMan"
	SingleNamespaceFlag   = "singleNamespace"
	TestNamespaceEnv      = "TEST_NAMESPACE"
	LocalOperatorFlag     = "localOperator"
)

func MainEntry(m *testing.M) {
	projRoot := flag.String(ProjRootFlag, "", "path to project root")
	kubeconfigPath := flag.String(KubeConfigFlag, "", "path to kubeconfig")
	globalManPath := flag.String(GlobalManPathFlag, "", "path to operator manifest")
	namespacedManPath := flag.String(NamespacedManPathFlag, "", "path to rbac manifest")
	singleNamespace = flag.Bool(SingleNamespaceFlag, false, "enable single namespace mode")
	localOperator := flag.Bool(LocalOperatorFlag, false, "enable if operator is running locally (not in cluster)")
	flag.Parse()
	// go test always runs from the test directory; change to project root
	err := os.Chdir(*projRoot)
	if err != nil {
		log.Fatalf("Failed to change directory to project root: %v", err)
	}
	if err := setup(kubeconfigPath, namespacedManPath, *localOperator); err != nil {
		log.Fatalf("Failed to set up framework: %v", err)
	}
	// setup local operator command, but don't start it yet
	var localCmd *exec.Cmd
	var localCmdOutBuf, localCmdErrBuf bytes.Buffer
	if *localOperator {
		absProjectPath := projutil.MustGetwd()
		projectName := filepath.Base(absProjectPath)
		outputBinName := filepath.Join(scaffold.BuildBinDir, projectName+"-local")
		args := []string{"build", "-o", outputBinName}
		args = append(args, filepath.Join(scaffold.ManagerDir, scaffold.CmdFile))
		bc := exec.Command("go", args...)
		if err := projutil.ExecCmd(bc); err != nil {
			log.Fatalf("Failed to build local operator binary: %s", err)
		}
		localCmd = exec.Command(outputBinName)
		localCmd.Stdout = &localCmdOutBuf
		localCmd.Stderr = &localCmdErrBuf
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			err := localCmd.Process.Signal(os.Interrupt)
			if err != nil {
				log.Fatalf("Failed to terminate the operator: (%v)", err)
			}
			os.Exit(0)
		}()
		if *kubeconfigPath != "" {
			localCmd.Env = append(os.Environ(), fmt.Sprintf("%v=%v", k8sutil.KubeConfigEnvVar, *kubeconfigPath))
		} else {
			// we can hardcode index 0 as that is the highest priority kubeconfig to be loaded and will always
			// be populated by NewDefaultClientConfigLoadingRules()
			localCmd.Env = append(os.Environ(), fmt.Sprintf("%v=%v", k8sutil.KubeConfigEnvVar, clientcmd.NewDefaultClientConfigLoadingRules().Precedence[0]))
		}
		localCmd.Env = append(localCmd.Env, fmt.Sprintf("%v=%v", k8sutil.WatchNamespaceEnvVar, Global.Namespace))
	}
	// setup context to use when setting up crd
	ctx := NewTestCtx(nil)
	// os.Exit stops the program before the deferred functions run
	// to fix this, we put the exit in the defer as well
	defer func() {
		// start local operator before running tests
		if *localOperator {
			err := localCmd.Start()
			if err != nil {
				log.Fatalf("Failed to run operator locally: (%v)", err)
			}
			log.Info("Started local operator")
		}
		exitCode := m.Run()
		if *localOperator {
			err := localCmd.Process.Kill()
			if err != nil {
				log.Warn("Failed to stop local operator process")
			}
			log.Infof("Local operator stdout: %s", string(localCmdOutBuf.Bytes()))
			log.Infof("Local operator stderr: %s", string(localCmdErrBuf.Bytes()))
		}
		ctx.Cleanup()
		os.Exit(exitCode)
	}()
	// create crd
	if *kubeconfigPath != "incluster" {
		globalYAML, err := ioutil.ReadFile(*globalManPath)
		if err != nil {
			log.Fatalf("Failed to read global resource manifest: %v", err)
		}
		err = ctx.createFromYAML(globalYAML, true, &CleanupOptions{TestContext: ctx})
		if err != nil {
			log.Fatalf("Failed to create resource(s) in global resource manifest: %v", err)
		}
	}
}
