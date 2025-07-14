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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"
)

func Test_newScopeLevelPair(t *testing.T) {
	validationPattern := `^[\w\- ]+:(none|error|warn|info|debug)`
	type args struct {
		slp               string
		validationPattern string
	}
	tests := []struct {
		name    string
		args    args
		want    *ScopeLevelPair
		wantErr bool
	}{
		{
			name:    "Fail when logs scope-level pair don't match pattern",
			args:    args{validationPattern: validationPattern, slp: "invalid:pattern"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newScopeLevelPair(tt.args.slp, tt.args.validationPattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("newScopeLevelPair() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newScopeLevelPair() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_newScopeStackTraceLevelPair(t *testing.T) {
	validationPattern := `^[\w\- ]+:(none|error|warn|info|debug)`
	type args struct {
		sslp              string
		validationPattern string
	}
	tests := []struct {
		name    string
		args    args
		want    *scopeStackTraceLevelPair
		wantErr bool
	}{
		{
			name:    "Fail when logs scope-level pair don't match pattern",
			args:    args{validationPattern: validationPattern, sslp: "invalid:pattern"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newScopeStackTraceLevelPair(tt.args.sslp, tt.args.validationPattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("newScopeStackTraceLevelPair() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newScopeStackTraceLevelPair() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_chooseClientFlag(t *testing.T) {
	url, _ := url.Parse("http://localhost/scopej/resource")

	ctrzClient := &ControlzClient{
		baseURL:    url,
		httpClient: &http.Client{},
	}

	type args struct {
		ctrzClient      *ControlzClient
		reset           bool
		logReset        bool
		stackTraceReset bool
		outputLogLevel  string
		stackTraceLevel string
		outputFormat    string
	}
	tests := []struct {
		name string
		args args
		want *istiodConfigLog
	}{
		{
			name: "given --reset flag return reset command",
			args: args{
				ctrzClient:      ctrzClient,
				reset:           true,
				logReset:        false,
				stackTraceReset: false,
				outputLogLevel:  "",
				stackTraceLevel: "",
				outputFormat:    "",
			},
			want: &istiodConfigLog{state: &resetState{
				client: ctrzClient,
			}},
		},
		{
			name: "given --log-reset flag return log reset command",
			args: args{
				ctrzClient:      ctrzClient,
				reset:           false,
				logReset:        true,
				stackTraceReset: false,
				outputLogLevel:  "",
				stackTraceLevel: "",
				outputFormat:    "",
			},
			want: &istiodConfigLog{state: &logResetState{
				client: ctrzClient,
			}},
		},
		{
			name: "given --stack-trace-reset flag return stackTraceReset command",
			args: args{
				ctrzClient:      ctrzClient,
				reset:           false,
				logReset:        false,
				stackTraceReset: true,
				outputLogLevel:  "",
				stackTraceLevel: "",
				outputFormat:    "",
			},
			want: &istiodConfigLog{state: &stackTraceResetState{
				client: ctrzClient,
			}},
		},
		{
			name: "given --log-reset and --stack-trace-reset flag return reset command",
			args: args{
				ctrzClient:      ctrzClient,
				reset:           false,
				logReset:        true,
				stackTraceReset: true,
				outputLogLevel:  "",
				stackTraceLevel: "",
				outputFormat:    "",
			},
			want: &istiodConfigLog{state: &resetState{
				client: ctrzClient,
			}},
		},
		{
			name: "given --level flag return outputLogLevel command",
			args: args{
				ctrzClient:      ctrzClient,
				reset:           false,
				logReset:        false,
				stackTraceReset: false,
				outputLogLevel:  "resource:info",
				stackTraceLevel: "",
				outputFormat:    "",
			},
			want: &istiodConfigLog{state: &logLevelState{
				client:         ctrzClient,
				outputLogLevel: "resource:info",
			}},
		},
		{
			name: "given --level flag containing '-' and none level return outputLogLevel command",
			args: args{
				ctrzClient:      ctrzClient,
				reset:           false,
				logReset:        false,
				stackTraceReset: false,
				outputLogLevel:  "resource-foo:none",
				stackTraceLevel: "",
				outputFormat:    "",
			},
			want: &istiodConfigLog{state: &logLevelState{
				client:         ctrzClient,
				outputLogLevel: "resource-foo:none",
			}},
		},
		{
			name: "given --stack-trace-level flag return stackTraceLevelState",
			args: args{
				ctrzClient:      ctrzClient,
				reset:           false,
				logReset:        false,
				stackTraceReset: false,
				outputLogLevel:  "",
				stackTraceLevel: "resource:info",
				outputFormat:    "",
			},
			want: &istiodConfigLog{
				state: &stackTraceLevelState{
					client:          ctrzClient,
					stackTraceLevel: "resource:info",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseClientFlag(tt.args.ctrzClient, tt.args.logReset, tt.args.stackTraceReset, tt.args.reset, tt.args.outputLogLevel,
				tt.args.stackTraceLevel, tt.args.outputFormat); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("chooseClientFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func resourceHandler(writer http.ResponseWriter, request *http.Request) {
	const getResponse = `{"name":"resource","description":"Core resource model scope","output_level":"info","stack_trace_level":"none","log_callers":false}`

	switch request.Method {
	case http.MethodGet:
		_, _ = writer.Write([]byte(getResponse))
	}
}

func adsHandler(writer http.ResponseWriter, request *http.Request) {
	const getResponse = `{"name":"ads","description":"ads debugging","output_level":"info","stack_trace_level":"none","log_callers":false}`

	switch request.Method {
	case http.MethodGet:
		_, _ = writer.Write([]byte(getResponse))
	}
}

func setupHTTPServer() (*httptest.Server, *url.URL) {
	handler := http.NewServeMux()
	handler.HandleFunc("/scopej/ads", adsHandler)
	handler.HandleFunc("/scopej/resource", resourceHandler)
	server := httptest.NewServer(handler)
	url, _ := url.Parse(server.URL)
	return server, url
}

func Test_flagState_run(t *testing.T) {
	server, url := setupHTTPServer()
	defer server.Close()

	ctrzClientNoScopejHandler := &ControlzClient{
		baseURL:    url,
		httpClient: &http.Client{},
	}
	tests := []struct {
		name    string
		state   flagState
		want    string
		wantErr bool
	}{
		{
			name:    "resetState.run() should throw an error if the /scopej endpoint is missing",
			state:   &resetState{client: ctrzClientNoScopejHandler},
			wantErr: true,
		},
		{
			name: "logLevelState.run() should throw an error if the /scopej endpoint is missing",
			state: &logLevelState{
				client:         ctrzClientNoScopejHandler,
				outputLogLevel: "test:debug",
			},
			wantErr: true,
		},
		{
			name: "stackTraceLevelState.run() should throw an error if the /scopej endpoint is missing",
			state: &stackTraceLevelState{
				client:          ctrzClientNoScopejHandler,
				stackTraceLevel: "test:debug",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.state.run(os.Stdout)
			if (err != nil) != tt.wantErr {
				t.Errorf("run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
