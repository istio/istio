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

package cmd

import (
	"bytes"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"istio.io/istio/istioctl/pkg/kubernetes"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/pkg/log"
	"net/http"
	"os/exec"
)

// Level is an enumeration of all supported log levels.
type Level int

const (
	// ErrorLevel enables error level logging
	ErrorLevel Level = iota
	// WarnLevel enables warn level logging
	WarnLevel
	// InfoLevel enables info level logging
	InfoLevel
	// DebugLevel enables debug level logging
	DebugLevel
)

var levelToString = map[Level]string{
	DebugLevel: "debug",
	InfoLevel:  "info",
	WarnLevel:  "warning",
	ErrorLevel: "error",
}

var stringToLevel = map[string]Level{
	"debug":   DebugLevel,
	"info":    InfoLevel,
	"warning": WarnLevel,
	"error":   ErrorLevel,
}

var (
	logLevelString = levelToString[WarnLevel]
	follow         = false
)

func pod() *cobra.Command {
	podCmd := &cobra.Command{
		Use:     "pod",
		Aliases: []string{"p"},
		Short:   "Update log level of istio-proxy in the specified pod, and get logs stream by optional.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("specify a pod")
			}

			logLevel, ok := stringToLevel[logLevelString]
			if !ok {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("unknown log level")
			}

			podName, ns := handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			fw, err := client.BuildPortForwarder(podName, ns, 0, 15000)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for %s: %v", podName, err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				// update envoy sidecar log level
				log.Debugf("port-forward to Envoy sidecar ready")
				_, err := http.Post(fmt.Sprintf("http://localhost:%d/logging?level=%s", fw.LocalPort, levelToString[logLevel]),
					"application/json", bytes.NewBufferString(""))
				if err != nil {
					return fmt.Errorf("failed to set log level")
				}
				close(fw.StopChannel)
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Update log level of pod %s to %v", podName, logLevel)
			if follow {
				execCommand("kubectl", cmd.OutOrStdout(), cmd.OutOrStderr(), "logs", "-f", podName, "-c", "istio-proxy")
			}
			return nil
		},
	}

	levelListString := fmt.Sprintf("[%s, %s, %s, %s]",
		levelToString[DebugLevel],
		levelToString[InfoLevel],
		levelToString[WarnLevel],
		levelToString[ErrorLevel])

	podCmd.PersistentFlags().StringVar(&logLevelString, "log_level", logLevelString,
		fmt.Sprintf("The minimum logging level of messages to output, can be one of %s", levelListString))
	podCmd.PersistentFlags().BoolVarP(&follow, "follow", "f", follow, "Specify if the logs should be streamed.")

	return podCmd
}

func execCommand(name string, Stdout io.Writer, Stderr io.Writer, arg ...string) {
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := exec.Command(name, arg...)
	stdoutIn, _ := cmd.StdoutPipe()
	stderrIn, _ := cmd.StderrPipe()
	var errStdout, errStderr error
	stdout := io.MultiWriter(Stdout, &stdoutBuf)
	stderr := io.MultiWriter(Stderr, &stderrBuf)
	err := cmd.Start()
	if err != nil {
		log.Fatalf("cmd.Start() failed with '%s'\n", err)
	}
	go func() {
		_, errStdout = io.Copy(stdout, stdoutIn)
	}()
	go func() {
		_, errStderr = io.Copy(stderr, stderrIn)
	}()
	err = cmd.Wait()
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
	}
	if errStdout != nil || errStderr != nil {
		log.Fatal("failed to capture stdout or stderr\n")
	}
}
