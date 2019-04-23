// Copyright 2019 Istio Authors.
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

package get

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ghodss/yaml"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/pkg/log"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions/resource"
)

var (
	outputters = map[string]func(io.Writer, []*resource.Info){
		"yaml":  printYaml,
		"short": printShort,
	}
	ns *string
	// output format (yaml or short)
	outputFormat     string
	getAllNamespaces bool
)

//NewGetCommand create a new command for listing Istio resources
func NewGetCommand() *cobra.Command {
	var (
		kubeConfigFlags = &genericclioptions.ConfigFlags{
			Context:    strPtr(""),
			Namespace:  strPtr(""),
			KubeConfig: strPtr(""),
		}
	)

	getCmd := &cobra.Command{
		Use:   "get <type> [<name>]",
		Short: "Get Istio resources",
		Long: `
		get retrieves Istio resources.

		If you do not specify installation file it will perform pre-check for your cluster
		and report whether the cluster is ready for Istio installation.
`,
		Example: `
		# List All Istio resources
		istioctl get all
		
		# List all virtual services
		istioctl get virtualservices
		
		# # Get a specific virtual service named bookinfo
		istioctl get virtualservice bookinfo
`,
		RunE: func(c *cobra.Command, args []string) error {
			argLen := len(args)
			if argLen < 1 || argLen > 2 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify the type of resource to get. Types are %v",
					strings.Join([]string{"all, todo"}, ", "))
			}
			getByName := argLen == 2
			getAll := strings.EqualFold(args[0], "all")
			if getAll && getByName {
				return errors.New("<all> can not be used with other options")
			}
			if getAllNamespaces && getByName {
				return errors.New("a resource cannot be retrieved by name across all namespaces")
			}
			ns = kubeConfigFlags.Namespace
			err := getIstioResources(*ns, kubeConfigFlags,
				c.OutOrStderr(), args, getAllNamespaces, outputFormat)
			if err != nil {
				return err
			}
			return nil
		},
	}
	flags := getCmd.PersistentFlags()
	kubeConfigFlags.AddFlags(flags)
	getCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "short",
		"Output format. One of:yaml|short")
	getCmd.PersistentFlags().BoolVar(&getAllNamespaces, "all-namespaces", false,
		"If present, list the requested object(s) across all namespaces. Namespace in current "+
			"context is ignored even if specified with --namespace.")
	return getCmd
}

func getIstioResources(namespace string,
	restClientGetter resource.RESTClientGetter, writer io.Writer,
	args []string, getAllNamespaces bool, outputFormat string) error {
	if namespace == "" {
		namespace = "default"
	}
	var typeorNameArgs []string
	if args[0] == "all" {
		typeorNameArgs = []string{strings.Join(generateResourceList(), ",")}
	} else {
		typeorNameArgs = args
	}
	var errs error
	r := resource.NewBuilder(restClientGetter).
		Unstructured().
		NamespaceParam(namespace).DefaultNamespace().AllNamespaces(getAllNamespaces).
		ResourceTypeOrNameArgs(true, typeorNameArgs...).
		ContinueOnError().
		Flatten().
		Do()
	if err := r.Err(); err != nil {
		errs = multierror.Append(errs, err)
	}
	infos, err := r.Infos()
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	if len(infos) == 0 {
		fmt.Fprintf(writer, "No resources found.")
		return errs
	}
	if outputFunc, ok := outputters[outputFormat]; ok {
		outputFunc(writer, infos)
		return nil
	}
	return fmt.Errorf("unknown output format %v. Types are yaml|short", outputFormat)
}

// Print as YAML
func printYaml(writer io.Writer, infos []*resource.Info) {
	for _, v := range infos {
		bytes, err := yaml.Marshal(v.Object)
		if err != nil {
			log.Errorf("Could not convert %v to YAML: %v", v.Name, err)
			continue
		}
		fmt.Fprint(writer, string(bytes))
		fmt.Fprintln(writer, "---")
	}
}

// Print a simple list of names
func printShort(writer io.Writer, infos []*resource.Info) {
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Mapping.GroupVersionKind.Kind < infos[j].Mapping.GroupVersionKind.Kind
	})
	prevType := ""
	w := tabwriter.NewWriter(writer, 13, 4, 2, ' ', 0)
	for _, v := range infos {
		gvk := v.Mapping.GroupVersionKind
		u := v.Object.(*unstructured.Unstructured)
		age := calcTimestamp(u.GetCreationTimestamp().Time)
		if prevType != gvk.Kind {
			if prevType != "" {
				// Place a newline between types when doing 'get all'
				fmt.Fprintf(w, "\n")
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				"Kind", "Name", "Namespace", "Group", "Version", "Age")
			prevType = gvk.Kind
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			gvk.Kind, v.Name, v.Namespace, gvk.Group, gvk.Version, age)

	}
	w.Flush()

}

func strPtr(val string) *string {
	return &val
}

func calcTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return "<unknown>"
	}

	seconds := int(time.Since(ts).Seconds())
	if seconds < -2 {
		return fmt.Sprintf("<invalid>")
	} else if seconds < 0 {
		return fmt.Sprintf("0s")
	} else if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}

	minutes := int(time.Since(ts).Minutes())
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}

	hours := int(time.Since(ts).Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh", hours)
	} else if hours < 365*24 {
		return fmt.Sprintf("%dd", hours/24)
	}
	return fmt.Sprintf("%dy", hours/24/365)
}

func generateResourceList() []string {
	res := make([]string, 0)
	for _, c := range metadata.Types.Collections() {
		s := strings.Split(c, "/")
		//special case
		if s[1] == "mixer" || s[1] == "policy" {
			s[1] = "config"
		}
		if s[0] == "k8s" {
			continue
		}
		t := strings.Join([]string{s[len(s)-1], s[1], s[0], "io"}, ".")
		res = append(res, t)
	}
	return res
}
