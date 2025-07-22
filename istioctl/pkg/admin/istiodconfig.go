// Copyright Istio Authors
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

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/completion"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/log"
)

type flagState interface {
	run(out io.Writer) error
}

var (
	_ flagState = (*resetState)(nil)
	_ flagState = (*levelState)(nil)
	_ flagState = (*getAllLogLevelsState)(nil)
)

type resetState struct {
	client *ControlzClient
}

func (rs *resetState) run(_ io.Writer) error {
	const (
		defaultOutputLevel     = "info"
		defaultStackTraceLevel = "none"
	)
	allScopes, err := rs.client.GetScopes()
	if err != nil {
		return fmt.Errorf("could not get all scopes: %v", err)
	}
	var defaultScopes []*ScopeInfo
	for _, scope := range allScopes {
		defaultScopes = append(defaultScopes, &ScopeInfo{
			Name:            scope.Name,
			OutputLevel:     defaultOutputLevel,
			StackTraceLevel: defaultStackTraceLevel,
		})
	}
	err = rs.client.PutScopes(defaultScopes)
	if err != nil {
		return err
	}

	return nil
}

type logResetState struct {
	client *ControlzClient
}

func (rs *logResetState) run(_ io.Writer) error {
	const (
		defaultOutputLevel = "info"
	)
	allScopes, err := rs.client.GetScopes()
	if err != nil {
		return fmt.Errorf("could not get all scopes: %v", err)
	}
	var defaultScopes []*ScopeInfo
	for _, scope := range allScopes {
		defaultScopes = append(defaultScopes, &ScopeInfo{
			Name:        scope.Name,
			OutputLevel: defaultOutputLevel,
		})
	}
	err = rs.client.PutScopes(defaultScopes)
	if err != nil {
		return err
	}

	return nil
}

type stackTraceResetState struct {
	client *ControlzClient
}

func (rs *stackTraceResetState) run(_ io.Writer) error {
	const (
		defaultStackTraceLevel = "none"
	)
	allScopes, err := rs.client.GetScopes()
	if err != nil {
		return fmt.Errorf("could not get all scopes: %v", err)
	}
	var defaultScopes []*ScopeInfo
	for _, scope := range allScopes {
		defaultScopes = append(defaultScopes, &ScopeInfo{
			Name:            scope.Name,
			StackTraceLevel: defaultStackTraceLevel,
		})
	}
	err = rs.client.PutScopes(defaultScopes)
	if err != nil {
		return err
	}

	return nil
}

type levelState struct {
	client          *ControlzClient
	outputLogLevel  string
	stackTraceLevel string
}

func (ll *levelState) run(_ io.Writer) error {
	var scopeInfos []*ScopeInfo
	if ll.outputLogLevel != "" {
		scopeLogInfos, err := newScopeInfosFromScopeLevelPairs(ll.outputLogLevel)
		if err != nil {
			return err
		}
		scopeInfos = append(scopeInfos, scopeLogInfos...)
	}
	if ll.stackTraceLevel != "" {
		scopeStackInfos, err := newScopeInfosFromScopeStackTraceLevelPairs(ll.stackTraceLevel)
		if err != nil {
			return err
		}
		scopeInfos = append(scopeInfos, scopeStackInfos...)
	}
	err := ll.client.PutScopes(scopeInfos)
	if err != nil {
		return err
	}
	return nil
}

type getAllLogLevelsState struct {
	client       *ControlzClient
	outputFormat string
}

func (ga *getAllLogLevelsState) run(out io.Writer) error {
	type scopeLogLevel struct {
		ScopeName       string `json:"scope_name"`
		LogLevel        string `json:"log_level"`
		StackTraceLevel string `json:"stack_trace_level"`
		Description     string `json:"description"`
	}
	allScopes, err := ga.client.GetScopes()
	sort.Slice(allScopes, func(i, j int) bool {
		return allScopes[i].Name < allScopes[j].Name
	})
	if err != nil {
		return fmt.Errorf("could not get scopes information: %v", err)
	}
	var resultScopeLogLevel []*scopeLogLevel
	for _, scope := range allScopes {
		resultScopeLogLevel = append(resultScopeLogLevel,
			&scopeLogLevel{
				ScopeName:       scope.Name,
				LogLevel:        scope.OutputLevel,
				StackTraceLevel: scope.StackTraceLevel,
				Description:     scope.Description,
			},
		)
	}
	switch ga.outputFormat {
	case "short":
		w := new(tabwriter.Writer).Init(out, 0, 8, 3, ' ', 0)
		_, _ = fmt.Fprintln(w, "ACTIVE SCOPE\tDESCRIPTION\tLOG LEVEL\tSTACK TRACE LEVEL")
		for _, sll := range resultScopeLogLevel {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", sll.ScopeName, sll.Description, sll.LogLevel, sll.StackTraceLevel)
		}
		return w.Flush()
	case "json", "yaml":
		outputBytes, err := json.MarshalIndent(&resultScopeLogLevel, "", "  ")
		outputBytes = append(outputBytes, []byte("\n")...)
		if err != nil {
			return err
		}
		if ga.outputFormat == "yaml" {
			if outputBytes, err = yaml.JSONToYAML(outputBytes); err != nil {
				return err
			}
		}
		_, err = out.Write(outputBytes)
		return err
	default:
		return fmt.Errorf("output format %q not supported", ga.outputFormat)
	}
}

type istiodConfigLog struct {
	state flagState
}

func (id *istiodConfigLog) execute(out io.Writer) error {
	return id.state.run(out)
}

func chooseClientFlag(ctrzClient *ControlzClient, logReset, stackTraceReset, reset bool, logLevel, stackTraceLevel, outputFormat string) *istiodConfigLog {
	switch {
	case reset || (logReset && stackTraceReset):
		return &istiodConfigLog{state: &resetState{ctrzClient}}
	case logReset:
		return &istiodConfigLog{state: &logResetState{ctrzClient}}
	case stackTraceReset:
		return &istiodConfigLog{state: &stackTraceResetState{ctrzClient}}
	case logLevel != "" || stackTraceLevel != "":
		return &istiodConfigLog{state: &levelState{
			client:          ctrzClient,
			outputLogLevel:  logLevel,
			stackTraceLevel: stackTraceLevel,
		}}
	default:
		return &istiodConfigLog{state: &getAllLogLevelsState{
			client:       ctrzClient,
			outputFormat: outputFormat,
		}}
	}
}

type ScopeInfo struct {
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	OutputLevel     string `json:"output_level,omitempty"`
	StackTraceLevel string `json:"stack_trace_level,omitempty"`
	LogCallers      bool   `json:"log_callers,omitempty"`
}

type ScopeLevelPair struct {
	scope    string
	logLevel string
}

type scopeStackTraceLevelPair ScopeLevelPair

func newScopeLevelPair(slp, validationPattern string) (*ScopeLevelPair, error) {
	matched, err := regexp.MatchString(validationPattern, slp)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, fmt.Errorf("pattern %s did not match", slp)
	}
	scopeLogLevel := strings.Split(slp, ":")
	s := &ScopeLevelPair{
		scope:    scopeLogLevel[0],
		logLevel: scopeLogLevel[1],
	}
	return s, nil
}

func newScopeInfosFromScopeLevelPairs(scopeLevelPairs string) ([]*ScopeInfo, error) {
	slParis := strings.Split(scopeLevelPairs, ",")
	var scopeInfos []*ScopeInfo
	for _, slp := range slParis {
		sl, err := newScopeLevelPair(slp, validationPattern)
		if err != nil {
			return nil, err
		}
		si := &ScopeInfo{
			Name:        sl.scope,
			OutputLevel: sl.logLevel,
		}
		scopeInfos = append(scopeInfos, si)
	}
	return scopeInfos, nil
}

func newScopeStackTraceLevelPair(sslp, validationPattern string) (*scopeStackTraceLevelPair, error) {
	matched, err := regexp.MatchString(validationPattern, sslp)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, fmt.Errorf("pattern %s did not match", sslp)
	}
	scopeStackTraceLevel := strings.Split(sslp, ":")
	ss := &scopeStackTraceLevelPair{
		scope:    scopeStackTraceLevel[0],
		logLevel: scopeStackTraceLevel[1],
	}
	return ss, nil
}

func newScopeInfosFromScopeStackTraceLevelPairs(scopeStackTraceLevelPairs string) ([]*ScopeInfo, error) {
	sslPairs := strings.Split(scopeStackTraceLevelPairs, ",")
	var scopeInfos []*ScopeInfo
	for _, sslp := range sslPairs {
		slp, err := newScopeStackTraceLevelPair(sslp, validationPattern)
		if err != nil {
			return nil, err
		}
		si := &ScopeInfo{
			Name:            slp.scope,
			StackTraceLevel: slp.logLevel,
		}
		scopeInfos = append(scopeInfos, si)
	}
	return scopeInfos, nil
}

type ControlzClient struct {
	baseURL    *url.URL
	httpClient *http.Client
}

func (c *ControlzClient) GetScopes() ([]*ScopeInfo, error) {
	var scopeInfos []*ScopeInfo
	resp, err := c.httpClient.Get(c.baseURL.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request not successful %s", resp.Status)
	}

	err = json.NewDecoder(resp.Body).Decode(&scopeInfos)
	if err != nil {
		return nil, fmt.Errorf("cannot deserialize response %s", err)
	}
	return scopeInfos, nil
}

func (c *ControlzClient) PutScope(scope *ScopeInfo) error {
	var jsonScopeInfo bytes.Buffer
	err := json.NewEncoder(&jsonScopeInfo).Encode(scope)
	if err != nil {
		return fmt.Errorf("cannot serialize scope %+v", *scope)
	}
	req, err := http.NewRequest(http.MethodPut, c.baseURL.String()+"/"+scope.Name, &jsonScopeInfo)
	if err != nil {
		return err
	}
	defer req.Body.Close()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("cannot update resource %s, got status %s", scope.Name, resp.Status)
	}
	return nil
}

func (c *ControlzClient) PutScopes(scopes []*ScopeInfo) error {
	ch := make(chan struct {
		err       error
		scopeName string
	}, len(scopes))
	var wg sync.WaitGroup
	for _, scope := range scopes {
		wg.Add(1)
		go func(si *ScopeInfo) {
			defer wg.Done()
			err := c.PutScope(si)
			ch <- struct {
				err       error
				scopeName string
			}{err: err, scopeName: si.Name}
		}(scope)
	}
	wg.Wait()
	close(ch)
	for result := range ch {
		if result.err != nil {
			return fmt.Errorf("failed updating Scope %s: %v", result.scopeName, result.err)
		}
	}
	return nil
}

func (c *ControlzClient) GetScope(scope string) (*ScopeInfo, error) {
	var s ScopeInfo
	resp, err := http.Get(c.baseURL.String() + "/" + scope)
	if err != nil {
		return &s, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &s, fmt.Errorf("request not successful %s: ", resp.Status)
	}

	err = json.NewDecoder(resp.Body).Decode(&s)
	if err != nil {
		return &s, fmt.Errorf("cannot deserialize response: %s", err)
	}
	return &s, nil
}

var (
	istiodLabelSelector = ""
	istiodReset         = false
	logReset            = false
	stackTraceReset     = false
	validationPattern   = `^[\w\- ]+:(none|error|warn|info|debug)`
)

func istiodLogCmd(ctx cli.Context) *cobra.Command {
	var controlzPort int
	var opts clioptions.ControlPlaneOptions
	outputLogLevel := ""
	stackTraceLevel := ""

	// output format (yaml or short)
	outputFormat := "short"

	logCmd := &cobra.Command{
		Use:   "log [<pod-name>]|[-r|--revision] [--level <scope>:<level>][--stack-trace-level <scope>:<level>]|[--reset]|[--output|-o short|json|yaml]",
		Short: "Manage istiod logging.",
		Long:  "Retrieve or update logging levels of istiod components.",
		Example: `  # Retrieve information about istiod logging levels.
  istioctl admin log

  # Retrieve information about istiod logging levels on a specific control plane pod.
  istioctl admin l istiod-5c868d8bdd-pmvgg

  # Update levels of the specified loggers.
  istioctl admin log --level ads:debug,authorization:debug

  # Retrieve information about istiod logging levels for a specified revision.
  istioctl admin log --revision v1

  # Reset levels of all the loggers to default value (info).
  istioctl admin log --reset
`,
		Aliases: []string{"l"},
		Args: func(logCmd *cobra.Command, args []string) error {
			if istiodReset && logReset && outputLogLevel != "" {
				logCmd.Println(logCmd.UsageString())
				return fmt.Errorf("--level cannot be combined with --reset, --log-reset")
			}
			if istiodReset && stackTraceReset && stackTraceLevel != "" {
				logCmd.Println(logCmd.UsageString())
				return fmt.Errorf("--stack-trace-level cannot be combined with --reset, --stack-trace-reset")
			}
			return nil
		},
		RunE: func(logCmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			var podName, ns string
			if len(args) == 0 {
				if opts.Revision == "" {
					opts.Revision = "default"
				}
				if len(istiodLabelSelector) > 0 {
					istiodLabelSelector = fmt.Sprintf("%s,%s=%s", istiodLabelSelector, label.IoIstioRev.Name, opts.Revision)
				} else {
					istiodLabelSelector = fmt.Sprintf("%s=%s", label.IoIstioRev.Name, opts.Revision)
				}
				pl, err := client.PodsForSelector(context.TODO(), ctx.NamespaceOrDefault(ctx.IstioNamespace()), istiodLabelSelector)
				if err != nil {
					return fmt.Errorf("not able to locate pod with selector %s: %v", istiodLabelSelector, err)
				}

				if len(pl.Items) < 1 {
					return errors.New("no pods found")
				}

				if len(pl.Items) > 1 {
					log.Warnf("more than 1 pods fits selector: %s; will use pod: %s", istiodLabelSelector, pl.Items[0].Name)
				}

				// only use the first pod in the list
				podName = pl.Items[0].Name
				ns = pl.Items[0].Namespace
			} else if len(args) == 1 {
				podName, ns = args[0], ctx.IstioNamespace()
			}

			portForwarder, err := client.NewPortForwarder(podName, ns, "", 0, controlzPort)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for ControlZ %s: %v", podName, err)
			}
			defer portForwarder.Close()
			err = portForwarder.Start()
			if err != nil {
				return fmt.Errorf("could not start port forwarder for ControlZ %s: %v", podName, err)
			}

			ctrlzClient := &ControlzClient{
				baseURL: &url.URL{
					Scheme: "http",
					Host:   portForwarder.Address(),
					Path:   "scopej",
				},
				httpClient: &http.Client{},
			}
			istiodConfigCmd := chooseClientFlag(ctrlzClient, logReset, stackTraceReset, istiodReset, outputLogLevel, stackTraceLevel, outputFormat)
			err = istiodConfigCmd.execute(logCmd.OutOrStdout())
			if err != nil {
				return err
			}
			return nil
		},
		ValidArgsFunction: completion.ValidPodsNameArgs(ctx),
	}
	opts.AttachControlPlaneFlags(logCmd)
	logCmd.PersistentFlags().BoolVar(&istiodReset, "reset", istiodReset, "Reset all levels to default value. (info)")
	logCmd.PersistentFlags().BoolVar(&logReset, "log-reset", logReset, "Reset log levels to default value. (info)")
	logCmd.PersistentFlags().BoolVar(&stackTraceReset, "stack-trace-reset", stackTraceReset, "Reset stack stace levels to default value. (none)")
	logCmd.PersistentFlags().IntVar(&controlzPort, "ctrlz_port", ctrlz.DefaultControlZPort, "ControlZ port")
	logCmd.PersistentFlags().StringVar(&outputLogLevel, "level", outputLogLevel,
		"Comma-separated list of output logging level for scopes in the format of <scope>:<level>[,<scope>:<level>,...]. "+
			"Possible values for <level>: none, error, warn, info, debug")
	logCmd.PersistentFlags().StringVar(&stackTraceLevel, "stack-trace-level", stackTraceLevel,
		"Comma-separated list of stack trace level for scopes in the format of <scope>:<stack-trace-level>[,<scope>:<stack-trace-level>,...]. "+
			"Possible values for <stack-trace-level>: none, error, warn, info, debug")
	logCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o",
		outputFormat, "Output format: one of json|yaml|short")
	return logCmd
}
