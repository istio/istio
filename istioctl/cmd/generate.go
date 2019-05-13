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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/template"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apilabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

var (
	templateVals []string

	plurals = map[string]string{
		"v1.Service":                    "services",
		"v1.Pod":                        "pods",
		"extensions/v1beta1.Deployment": "deployments",

		"networking.istio.io/v1alpha3.Gateway":         "gateways",
		"networking.istio.io/v1alpha3.VirtualService":  "virtualservices",
		"networking.istio.io/v1alpha3.DestinationRule": "destinationrules",
		"networking.istio.io/v1alpha3.ServiceEntry":    "serviceentries",
		// Add other types needed by templates here
	}

	funcMap = template.FuncMap{
		"required":   required,
		"quote":      quote,
		"default":    defaultFunc,
		"parseBool":  parseBool,
		"k8s_list":   k8sList,
		"k8s_get":    k8sGet,
		"istio_mtls": istioMtls,
	}

	// mock point for dynamic.Interface factory
	dynamicClientFactory = newDynamicClient
)

func generateCommand() *cobra.Command {
	generateCmd := &cobra.Command{
		Use:   "generate [<template-configmap>] [-f <filename>] [--set key=value]*",
		Short: "Generate Istio configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, positionalArgs []string) error {
			if len(positionalArgs) == 0 && file == "" {
				return listGenerators(c)
			}

			var tmplSrc string
			var err error
			if len(positionalArgs) == 1 {
				if file != "" {
					return fmt.Errorf("supply either a configmap or --file parameter, not both")
				}
				tmplSrc, err = getTemplate(positionalArgs[0])
			} else {
				tmplSrc, err = readTemplate()
			}
			if err != nil {
				return err
			}

			tmpl, err := template.New("generate").Funcs(funcMap).Parse(tmplSrc)
			if err != nil {
				return err
			}

			parameters := make(map[string]string)
			for _, v := range templateVals {
				kv := strings.Split(v, "=")
				if len(kv) != 2 || kv[0] == "" || kv[1] == "" {
					return fmt.Errorf("%q not in key=value format", v)
				}
				parameters[kv[0]] = strings.Join(kv[1:], "=")
			}

			var out bytes.Buffer
			err = tmpl.Execute(&out, parameters)
			if err != nil {
				return err
			}

			fmt.Fprintf(c.OutOrStdout(), "%s", out.String())

			return nil
		},
	}

	generateCmd.PersistentFlags().StringVarP(&file, "file", "f",
		"", "Input template filename")
	generateCmd.PersistentFlags().StringSliceVar(&templateVals, "set",
		[]string{}, "template arguments")
	return generateCmd
}

// readInputs reads multiple documents from the input and checks with the schema
func readTemplate() (string, error) {
	var reader io.Reader
	switch file {
	case "":
		return "", errors.New("filename not specified (see --file or -f)")
	case "-":
		reader = os.Stdin
	default:
		var err error
		var in *os.File
		if in, err = os.Open(file); err != nil {
			return "", err
		}
		defer func() {
			if err = in.Close(); err != nil {
				log.Errorf("Error: close file from %s, %s", file, err)
			}
		}()
		reader = in
	}
	input, err := ioutil.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(input), nil
}

func required(msg string, v interface{}) (string, error) {
	if v != nil && v != "" {
		return fmt.Sprintf("%v", v), nil
	}
	return "", errors.New(msg)
}

func quote(s string) string {
	return fmt.Sprintf("%q", s)
}

func newDynamicClient() (dynamic.Interface, error) {
	restConfig, err := kube.BuildClientConfig(kubeconfig, configContext)
	if err != nil {
		return nil, err
	}

	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Given "networking.istio.io/v1alpha3.Gateway", return "networking.istio.io"
func k8sGroup(typ string) string {
	sa := strings.Split(typ, "/")
	if len(sa) < 2 {
		return ""
	}
	return sa[0]
}

// Given "networking.istio.io/v1alpha3.Gateway", return "v1alpha3"
func k8sVersion(typ string) string {
	sa := strings.Split(typ, "/")
	if len(sa) == 0 {
		return ""
	}
	v := sa[len(sa)-1]
	va := strings.Split(v, ".")
	if len(va) < 1 {
		return ""
	}
	return va[0]

}

func gvr(k8sType string) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    k8sGroup(k8sType),
		Version:  k8sVersion(k8sType),
		Resource: k8sPlural(k8sType),
	}
}

func k8sList(typ string) (*[]map[string]interface{}, error) {
	if k8sType(typ) {
		client, err := dynamicClientFactory()
		if err != nil {
			return nil, err
		}
		ul, err := client.Resource(gvr(typ)).Namespace(effectiveNamespace(namespace)).List(metav1.ListOptions{})
		if err != nil {
			return nil, multierror.Prefix(err, fmt.Sprintf("Retrieving %s", typ))
		}

		retval := make([]map[string]interface{}, len(ul.Items))
		for i, item := range ul.Items {
			retval[i] = item.UnstructuredContent()
		}
		return &retval, nil
	}

	return nil, fmt.Errorf("unknown type %q", typ)
}

func defaultFunc(val, defval interface{}) (string, error) {
	if val != nil && val != "" {
		return fmt.Sprintf("%v", val), nil
	}

	return fmt.Sprintf("%v", defval), nil
}

func k8sGet(typ, name string) (*map[string]interface{}, error) {
	if k8sType(typ) {
		client, err := dynamicClientFactory()
		if err != nil {
			return nil, err
		}
		u, err := client.Resource(gvr(typ)).Namespace(effectiveNamespace(namespace)).Get(name, metav1.GetOptions{})
		if err != nil {
			return nil, multierror.Prefix(err, fmt.Sprintf("Unknown %s %q", typ, name))
		}

		retval := u.UnstructuredContent()
		return &retval, nil
	}

	return nil, fmt.Errorf("unknown type %q", typ)
}

func k8sType(typ string) bool {
	_, ok := plurals[typ]
	return ok
}

func k8sPlural(typ string) string {
	plural := plurals[typ]
	return plural
}

func parseBool(val interface{}) (bool, error) {
	if val == nil {
		return false, nil
	}
	return strconv.ParseBool(fmt.Sprintf("%v", val))
}

// Note that this only checks global mTLS, not the namespace settings for svc
func istioMtls(svc string) (bool, error) {
	istioClient, err := clientFactory()
	if err != nil {
		return false, err
	}

	meshPolicy := istioClient.Get("mesh-policy", "default", istioNamespace)
	if meshPolicy == nil {
		return false, fmt.Errorf("no Istio MeshPolicy defined")
	}

	policy := meshPolicy.Spec.(*authn.Policy)
	for _, peer := range policy.Peers {
		_, ok := peer.Params.(*authn.PeerAuthenticationMethod_Mtls)
		if ok {
			return true, nil
		}
	}

	return false, nil
}

func listGenerators(c *cobra.Command) error {

	client, err := createInterface(kubeconfig)
	if err != nil {
		return err
	}

	configs, err := client.CoreV1().ConfigMaps(istioNamespace).List(metav1.ListOptions{
		LabelSelector: apilabels.SelectorFromSet(map[string]string{"tag": "gen-template"}).String(),
	})
	if err != nil {
		return fmt.Errorf("could not read configmaps from namespace %q: %v",
			istioNamespace, err)
	}

	printGenerators(c.OutOrStdout(), configs.Items)

	return nil
}

func printGenerators(writer io.Writer, configmaps []corev1.ConfigMap) {
	sort.Slice(configmaps, func(i, j int) bool {
		return configmaps[i].ObjectMeta.Name < configmaps[j].ObjectMeta.Name
	})

	var printedHeader bool
	for _, configmap := range configmaps {
		if !printedHeader {
			fmt.Fprintf(writer, "NAME\n")
			printedHeader = true
		}

		fmt.Fprintf(writer, "%s\n", configmap.ObjectMeta.Name)
	}
	if !printedHeader {
		fmt.Fprintf(writer, "No generators found.")
	}
}

func getTemplate(templateName string) (string, error) {
	client, err := createInterface(kubeconfig)
	if err != nil {
		return "", err
	}

	config, err := client.CoreV1().ConfigMaps(istioNamespace).Get(templateName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("could not read valid configmap %q from namespace %q: %v",
			templateName, istioNamespace, err)
	}

	for _, contents := range config.Data {
		return contents, nil
	}

	return "", fmt.Errorf("no template in ConfigMap")
}

func effectiveNamespace(namespace string) string {
	if namespace != "" {
		return namespace
	}

	return defaultNamespace
}
