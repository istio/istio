/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/gosuri/uitable"
	"github.com/spf13/cobra"

	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/timeconv"
)

var listHelp = `
This command lists all of the releases.

By default, it lists only releases that are deployed or failed. Flags like
'--deleted' and '--all' will alter this behavior. Such flags can be combined:
'--deleted --failed'.

By default, items are sorted alphabetically. Use the '-d' flag to sort by
release date.

If an argument is provided, it will be treated as a filter. Filters are
regular expressions (Perl compatible) that are applied to the list of releases.
Only items that match the filter will be returned.

	$ helm list 'ara[a-z]+'
	NAME            	UPDATED                 	CHART
	maudlin-arachnid	Mon May  9 16:07:08 2016	alpine-0.1.0

If no results are found, 'helm list' will exit 0, but with no output (or in
the case of no '-q' flag, only headers).

By default, up to 256 items may be returned. To limit this, use the '--max' flag.
Setting '--max' to 0 will not return all results. Rather, it will return the
server's default, which may be much higher than 256. Pairing the '--max'
flag with the '--offset' flag allows you to page through results.
`

type listCmd struct {
	filter      string
	short       bool
	limit       int
	offset      string
	byDate      bool
	sortDesc    bool
	out         io.Writer
	all         bool
	deleted     bool
	deleting    bool
	deployed    bool
	failed      bool
	namespace   string
	superseded  bool
	pending     bool
	client      helm.Interface
	colWidth    uint
	output      string
	byChartName bool
}

type listResult struct {
	Next     string
	Releases []listRelease
}

type listRelease struct {
	Name       string
	Revision   int32
	Updated    string
	Status     string
	Chart      string
	AppVersion string
	Namespace  string
}

func newListCmd(client helm.Interface, out io.Writer) *cobra.Command {
	list := &listCmd{
		out:    out,
		client: client,
	}

	cmd := &cobra.Command{
		Use:     "list [flags] [FILTER]",
		Short:   "list releases",
		Long:    listHelp,
		Aliases: []string{"ls"},
		PreRunE: func(_ *cobra.Command, _ []string) error { return setupConnection() },
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				list.filter = strings.Join(args, " ")
			}
			if list.client == nil {
				list.client = newClient()
			}
			return list.run()
		},
	}

	f := cmd.Flags()
	settings.AddFlagsTLS(f)
	f.BoolVarP(&list.short, "short", "q", false, "output short (quiet) listing format")
	f.BoolVarP(&list.byDate, "date", "d", false, "sort by release date")
	f.BoolVarP(&list.sortDesc, "reverse", "r", false, "reverse the sort order")
	f.IntVarP(&list.limit, "max", "m", 256, "maximum number of releases to fetch")
	f.StringVarP(&list.offset, "offset", "o", "", "next release name in the list, used to offset from start value")
	f.BoolVarP(&list.all, "all", "a", false, "show all releases, not just the ones marked DEPLOYED")
	f.BoolVar(&list.deleted, "deleted", false, "show deleted releases")
	f.BoolVar(&list.deleting, "deleting", false, "show releases that are currently being deleted")
	f.BoolVar(&list.deployed, "deployed", false, "show deployed releases. If no other is specified, this will be automatically enabled")
	f.BoolVar(&list.failed, "failed", false, "show failed releases")
	f.BoolVar(&list.pending, "pending", false, "show pending releases")
	f.StringVar(&list.namespace, "namespace", "", "show releases within a specific namespace")
	f.UintVar(&list.colWidth, "col-width", 60, "specifies the max column width of output")
	f.StringVar(&list.output, "output", "", "output the specified format (json or yaml)")
	f.BoolVarP(&list.byChartName, "chart-name", "c", false, "sort by chart name")

	// TODO: Do we want this as a feature of 'helm list'?
	//f.BoolVar(&list.superseded, "history", true, "show historical releases")

	// set defaults from environment
	settings.InitTLS(f)

	return cmd
}

func (l *listCmd) run() error {
	sortBy := services.ListSort_NAME
	if l.byDate {
		sortBy = services.ListSort_LAST_RELEASED
	}
	if l.byChartName {
		sortBy = services.ListSort_CHART_NAME
	}

	sortOrder := services.ListSort_ASC
	if l.sortDesc {
		sortOrder = services.ListSort_DESC
	}

	stats := l.statusCodes()

	res, err := l.client.ListReleases(
		helm.ReleaseListLimit(l.limit),
		helm.ReleaseListOffset(l.offset),
		helm.ReleaseListFilter(l.filter),
		helm.ReleaseListSort(int32(sortBy)),
		helm.ReleaseListOrder(int32(sortOrder)),
		helm.ReleaseListStatuses(stats),
		helm.ReleaseListNamespace(l.namespace),
	)

	if err != nil {
		return prettyError(err)
	}
	if res == nil {
		return nil
	}

	rels := filterList(res.GetReleases())

	result := getListResult(rels, res.Next)

	output, err := formatResult(l.output, l.short, result, l.colWidth)

	if err != nil {
		return prettyError(err)
	}

	fmt.Fprintln(l.out, output)
	return nil
}

// filterList returns a list scrubbed of old releases.
func filterList(rels []*release.Release) []*release.Release {
	idx := map[string]int32{}

	for _, r := range rels {
		name, version := r.GetName(), r.GetVersion()
		if max, ok := idx[name]; ok {
			// check if we have a greater version already
			if max > version {
				continue
			}
		}
		idx[name] = version
	}

	uniq := make([]*release.Release, 0, len(idx))
	for _, r := range rels {
		if idx[r.GetName()] == r.GetVersion() {
			uniq = append(uniq, r)
		}
	}
	return uniq
}

// statusCodes gets the list of status codes that are to be included in the results.
func (l *listCmd) statusCodes() []release.Status_Code {
	if l.all {
		return []release.Status_Code{
			release.Status_UNKNOWN,
			release.Status_DEPLOYED,
			release.Status_DELETED,
			release.Status_DELETING,
			release.Status_FAILED,
			release.Status_PENDING_INSTALL,
			release.Status_PENDING_UPGRADE,
			release.Status_PENDING_ROLLBACK,
		}
	}
	status := []release.Status_Code{}
	if l.deployed {
		status = append(status, release.Status_DEPLOYED)
	}
	if l.deleted {
		status = append(status, release.Status_DELETED)
	}
	if l.deleting {
		status = append(status, release.Status_DELETING)
	}
	if l.failed {
		status = append(status, release.Status_FAILED)
	}
	if l.superseded {
		status = append(status, release.Status_SUPERSEDED)
	}
	if l.pending {
		status = append(status, release.Status_PENDING_INSTALL, release.Status_PENDING_UPGRADE, release.Status_PENDING_ROLLBACK)
	}

	// Default case.
	if len(status) == 0 {
		status = append(status, release.Status_DEPLOYED, release.Status_FAILED)
	}
	return status
}

func getListResult(rels []*release.Release, next string) listResult {
	listReleases := []listRelease{}
	for _, r := range rels {
		md := r.GetChart().GetMetadata()
		t := "-"
		if tspb := r.GetInfo().GetLastDeployed(); tspb != nil {
			t = timeconv.String(tspb)
		}

		lr := listRelease{
			Name:       r.GetName(),
			Revision:   r.GetVersion(),
			Updated:    t,
			Status:     r.GetInfo().GetStatus().GetCode().String(),
			Chart:      fmt.Sprintf("%s-%s", md.GetName(), md.GetVersion()),
			AppVersion: md.GetAppVersion(),
			Namespace:  r.GetNamespace(),
		}
		listReleases = append(listReleases, lr)
	}

	return listResult{
		Releases: listReleases,
		Next:     next,
	}
}

func shortenListResult(result listResult) []string {
	names := []string{}
	for _, r := range result.Releases {
		names = append(names, r.Name)
	}

	return names
}

func formatResult(format string, short bool, result listResult, colWidth uint) (string, error) {
	var output string
	var err error

	var shortResult []string
	var finalResult interface{}
	if short {
		shortResult = shortenListResult(result)
		finalResult = shortResult
	} else {
		finalResult = result
	}

	switch format {
	case "":
		if short {
			output = formatTextShort(shortResult)
		} else {
			output = formatText(result, colWidth)
		}
	case "json":
		o, e := json.Marshal(finalResult)
		if e != nil {
			err = fmt.Errorf("Failed to Marshal JSON output: %s", e)
		} else {
			output = string(o)
		}
	case "yaml":
		o, e := yaml.Marshal(finalResult)
		if e != nil {
			err = fmt.Errorf("Failed to Marshal YAML output: %s", e)
		} else {
			output = string(o)
		}
	default:
		err = fmt.Errorf("Unknown output format \"%s\"", format)
	}
	return output, err
}

func formatText(result listResult, colWidth uint) string {
	nextOutput := ""
	if result.Next != "" {
		nextOutput = fmt.Sprintf("\tnext: %s\n", result.Next)
	}

	table := uitable.New()
	table.MaxColWidth = colWidth
	table.AddRow("NAME", "REVISION", "UPDATED", "STATUS", "CHART", "APP VERSION", "NAMESPACE")
	for _, lr := range result.Releases {
		table.AddRow(lr.Name, lr.Revision, lr.Updated, lr.Status, lr.Chart, lr.AppVersion, lr.Namespace)
	}

	return fmt.Sprintf("%s%s", nextOutput, table.String())
}

func formatTextShort(shortResult []string) string {
	return strings.Join(shortResult, "\n")
}
