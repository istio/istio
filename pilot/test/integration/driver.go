// Copyright 2017 Google Inc.
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

// Basic template engine using go templates

package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/clientcmd"

	flag "github.com/spf13/pflag"
)

func write(in string, data map[string]string, out io.Writer) error {
	tmpl, err := template.ParseFiles(in)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(out, data); err != nil {
		return err
	}
	return nil
}

const (
	yaml      = "echo.yaml"
	namespace = "default"
)

func check(err error) {
	if err != nil {
		log.Fatalf(err.Error())
	}
}

var (
	hub    string
	tag    string
	client *kubernetes.Clientset
)

func init() {
	flag.StringVarP(&hub, "hub", "h", "gcr.io/istio-test", "Docker hub")
	flag.StringVarP(&tag, "tag", "t", "test", "Docker tag")
}

func main() {
	flag.Parse()
	create := true

	// write template
	f, err := os.Create(yaml)
	check(err)
	w := bufio.NewWriter(f)

	check(write("test/integration/manager.yaml.tmpl", map[string]string{
		"hub": hub,
		"tag": tag,
	}, w))

	check(write("test/integration/http-service.yaml.tmpl", map[string]string{
		"hub":   hub,
		"tag":   tag,
		"name":  "a",
		"port1": "8080",
		"port2": "80",
	}, w))

	check(write("test/integration/http-service.yaml.tmpl", map[string]string{
		"hub":   hub,
		"tag":   tag,
		"name":  "b",
		"port1": "80",
		"port2": "8000",
	}, w))

	check(w.Flush())
	check(f.Close())

	// push docker images
	if create {
		run("gcloud docker --authorize-only")
		for _, image := range []string{"app", "runtime"} {
			run(fmt.Sprintf("bazel run //docker:%s", image))
			run(fmt.Sprintf("docker tag istio/docker:%s %s/%s:%s", image, hub, image, tag))
			run(fmt.Sprintf("docker push %s/%s:%s", hub, image, tag))
		}
		run("kubectl apply -f " + yaml + " -n " + namespace)
	}
	/*
	   # Wait for pods to be ready
	   while : ; do
	     kubectl get pods | grep -i "init\|creat\|error" || break
	     sleep 1
	   done

	   # Get pod names
	   for pod in a b t; do
	     declare "${pod}"="$(kubectl get pods -l app=$pod -o jsonpath='{range .items[*]}{@.metadata.name}')"
	   done
	*/

	client = connect()
	pods := getPods()
	log.Println("pods:", pods)
	ids := makeRequests(pods)
	log.Println("requests:", ids)
	checkAccessLogs(pods, ids)
	log.Println("Success!")
}

func run(command string) {
	log.Println("run", command)
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	check(c.Run())
}

func shell(command string) string {
	log.Println("exec", command)
	parts := strings.Split(command, " ")
	/* #nosec */
	c := exec.Command(parts[0], parts[1:]...)
	bytes, err := c.CombinedOutput()
	if err != nil {
		log.Fatal(string(bytes), err)
	}
	return string(bytes)
}

func connect() *kubernetes.Clientset {
	config, err := clientcmd.BuildConfigFromFlags("", "platform/kube/config")
	check(err)
	cl, err := kubernetes.NewForConfig(config)
	check(err)
	return cl
}

// pods returns pod names by app label as soon as all pods are ready
func getPods() map[string]string {
	pods := make([]v1.Pod, 0)
	out := make(map[string]string)
	n := 0
	for {
		log.Println("Checking all pods are running...")
		n = n + 1
		list, err := client.Pods(namespace).List(v1.ListOptions{})
		check(err)
		pods = list.Items
		ready := true

		for _, pod := range pods {
			if pod.Status.Phase != "Running" {
				log.Printf("Pod %s has status %s\n", pod.Name, pod.Status.Phase)
				ready = false
				break
			}
		}

		if ready {
			break
		}

		if n > 30 {
			log.Fatal("Exceeded budget for checking pod status")
		}

		time.Sleep(1 * time.Second)
	}

	for _, pod := range pods {
		if app, exists := pod.Labels["app"]; exists {
			out[app] = pod.Name
		}
	}

	return out
}

// makeRequests executes requests in each pod and collects request ids per pod
func makeRequests(pods map[string]string) map[string][]string {
	out := make(map[string][]string)
	for app := range pods {
		out[app] = make([]string, 0)
	}

	for src := range pods {
		for dst := range pods {
			for _, port := range []string{"", ":80", ":8080"} {
				for _, domain := range []string{"", "." + namespace} {
					url := fmt.Sprintf("http://%s%s%s/%s", dst, domain, port, src)
					log.Printf("Making a request %s from %s...\n", url, src)
					request := shell(fmt.Sprintf("kubectl exec %s -c app client %s", pods[src], url))
					log.Println(request)
					match := regexp.MustCompile("X-Request-Id=(.*)").FindStringSubmatch(request)
					if len(match) > 1 {
						id := match[1]
						log.Printf("id=%s\n", id)
						out[src] = append(out[src], id)
						out[dst] = append(out[dst], id)
					} else if src != "t" || dst != "t" {
						log.Fatalf("Failed to inject proxy from %s to %s (url %s)", src, dst, url)
					}
				}
			}
		}
	}

	return out
}

func checkAccessLogs(pods map[string]string, ids map[string][]string) {
	n := 0
	for {
		found := true
		n = n + 1
		for _, pod := range []string{"a", "b"} {
			log.Printf("Checking access log of %s\n", pod)
			access := shell(fmt.Sprintf("kubectl logs %s -c proxy", pods[pod]))
			for _, id := range ids[pod] {
				if !strings.Contains(access, id) {
					log.Printf("Failed to find request id %s in log of %s\n", id, pod)
					found = false
					break
				}
			}
			if !found {
				break
			}
		}

		if found {
			break
		}

		if n > 30 {
			log.Fatalf("Exceeded budget for checking access logs")
		}

		time.Sleep(1 * time.Second)
	}
}
