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

// Package redisquota provides a quota implementation with redis as backend.
// The prerequisite is to have a redis server running.

package redisquota

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis"
	"github.com/alicebob/miniredis/server"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/mixer/adapter/redisquota/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/quota"
)

type (
	RequestInfo struct {
		bestEffort      bool
		token           int64
		timestamp       int64
		allocated       int64
		validDuration   int64
		deduplicationid string
	}
)

func TestBuilderValidate(t *testing.T) {
	mockRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Unable to start mock redis server: %v", err)
		return
	}
	defer mockRedis.Close()

	cases := map[string]struct {
		quotaTypes map[string]*quota.Type
		config     *config.Params
		errMsg     []string
	}{
		"Empty redis server url": {
			config: &config.Params{
				RedisServerUrl:     "",
				ConnectionPoolSize: 10,
			},
			errMsg: []string{
				"quotas: quota should not be empty",
				"redis_server_url: redis_server_url should not be empty",
				"redisquota: could not create a connection to redis server: dial tcp: missing address",
			},
		},
		"Invalid connection pool size": {
			config: &config.Params{
				RedisServerUrl:     "",
				ConnectionPoolSize: -10,
			},
			errMsg: []string{
				"quotas: quota should not be empty",
				"connection_pool_size: connection_pool_size of -10 is invalid, must be > 0",
				"redis_server_url: redis_server_url should not be empty",
				"redisquota: could not create a connection to redis server: dial tcp: missing address",
			},
		},
		"Empty quota config": {
			config: &config.Params{
				RedisServerUrl:     mockRedis.Addr(),
				ConnectionPoolSize: 10,
			},
			errMsg: []string{
				"quotas: quota should not be empty",
			},
		},
		"Empty quota name": {
			quotaTypes: map[string]*quota.Type{
				"rolling-window": {},
			},
			config: &config.Params{
				RedisServerUrl: mockRedis.Addr(),
				Quotas: []config.Params_Quota{
					{},
				},
			},
			errMsg: []string{
				"name: quotas.name should not be empty",
				"quotas: did not find limit defined for quota rolling-window",
			},
		},
		"Invalid quota.max_amount": {
			quotaTypes: map[string]*quota.Type{
				"fixed-window": {},
			},
			config: &config.Params{
				RedisServerUrl: mockRedis.Addr(),
				Quotas: []config.Params_Quota{
					{
						Name: "fixed-window",
					},
				},
			},
			errMsg: []string{
				"valid_duration: quotas.valid_duration should be bigger must be > 0",
			},
		},
		"quota.valid_duration is not defined": {
			quotaTypes: map[string]*quota.Type{
				"rolling-window": {},
			},
			config: &config.Params{
				RedisServerUrl: mockRedis.Addr(),
				Quotas: []config.Params_Quota{
					{
						Name:      "rolling-window",
						MaxAmount: 10,
					},
				},
			},
			errMsg: []string{
				"valid_duration: quotas.valid_duration should be bigger must be > 0",
			},
		},
		"quota.bucket_duration is not defined": {
			quotaTypes: map[string]*quota.Type{
				"rolling-window": {},
			},
			config: &config.Params{
				RedisServerUrl: mockRedis.Addr(),
				Quotas: []config.Params_Quota{
					{
						Name:               "rolling-window",
						MaxAmount:          10,
						ValidDuration:      time.Minute,
						RateLimitAlgorithm: config.ROLLING_WINDOW,
					},
				},
			},
			errMsg: []string{
				"bucket_duration: quotas.bucket_duration should be > 0 for ROLLING_WINDOW algorithm",
			},
		},
		"quota.bucket_duration is longer than quota.valid_duration": {
			quotaTypes: map[string]*quota.Type{
				"rolling-window": {},
			},
			config: &config.Params{
				RedisServerUrl: mockRedis.Addr(),
				Quotas: []config.Params_Quota{
					{
						Name:               "rolling-window",
						MaxAmount:          10,
						ValidDuration:      time.Minute,
						BucketDuration:     time.Minute * time.Duration(2),
						RateLimitAlgorithm: config.ROLLING_WINDOW,
					},
				},
			},
			errMsg: []string{
				"valid_duration: quotas.valid_duration: 1m0s should be longer than quotas.bucket_duration: 2m0s for ROLLING_WINDOW algorithm",
			},
		},
		"Valid rolling window configuration": {
			quotaTypes: map[string]*quota.Type{
				"rolling-window": {},
			},
			config: &config.Params{
				RedisServerUrl: mockRedis.Addr(),
				Quotas: []config.Params_Quota{
					{
						Name:               "rolling-window",
						MaxAmount:          10,
						ValidDuration:      time.Minute,
						BucketDuration:     time.Second,
						RateLimitAlgorithm: config.ROLLING_WINDOW,
					},
				},
			},
			errMsg: []string{},
		},
		"Override invalid max_amount": {
			quotaTypes: map[string]*quota.Type{
				"rolling-window": {},
			},
			config: &config.Params{
				RedisServerUrl: mockRedis.Addr(),
				Quotas: []config.Params_Quota{
					{
						Name:               "rolling-window",
						MaxAmount:          10,
						ValidDuration:      time.Minute,
						BucketDuration:     time.Second,
						RateLimitAlgorithm: config.ROLLING_WINDOW,
						Overrides: []*config.Params_Override{
							{},
						},
					},
				},
			},
			errMsg: []string{
				"max_amount: quotas.overrides.max_amount must be > 0",
			},
		},
		"Override empty dimensions": {
			quotaTypes: map[string]*quota.Type{
				"rolling-window": {},
			},
			config: &config.Params{
				RedisServerUrl: mockRedis.Addr(),
				Quotas: []config.Params_Quota{
					{
						Name:               "rolling-window",
						MaxAmount:          10,
						ValidDuration:      time.Minute,
						BucketDuration:     time.Second,
						RateLimitAlgorithm: config.ROLLING_WINDOW,
						Overrides: []*config.Params_Override{
							{
								MaxAmount: 5,
							},
						},
					},
				},
			},
			errMsg: []string{
				"dimensions: quotas.overrides.dimensions is empty",
			},
		},
	}

	info := GetInfo()

	for id, c := range cases {
		b := info.NewBuilder().(*builder)
		b.SetAdapterConfig(c.config)
		b.SetQuotaTypes(c.quotaTypes)

		ce := b.Validate()
		if ce != nil {
			if len(ce.Multi.Errors) != len(c.errMsg) {
				t.Errorf("%v: Invalid number of errors. Expected %v got %v", id, len(c.errMsg), len(ce.Multi.Errors))
			} else {
				for idx, err := range ce.Multi.Errors {
					if c.errMsg[idx] != err.Error() {
						t.Errorf("%v: Invalid error message. Expected %v got %v", id, c.errMsg[idx], err.Error())
					}
				}
			}
		} else if len(c.errMsg) > 0 {
			t.Errorf("%v: Succeeded. Error expected", id)
		}
	}
}

func TestHandleQuota(t *testing.T) {
	mockRedis, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Unable to start mock redis server: %v", err)
		return
	}
	defer mockRedis.Close()

	info := GetInfo()

	if !contains(info.SupportedTemplates, quota.TemplateName) {
		t.Error("Didn't find all expected supported templates")
	}

	cases := map[string]struct {
		quotaType   map[string]*quota.Type
		quotaConfig []config.Params_Quota
		instance    quota.Instance
		request     []RequestInfo
	}{
		"algorithm = fixed window, best effort = true": {
			quotaType: map[string]*quota.Type{
				"fixed_window_best_effort": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name:               "fixed_window_best_effort",
					MaxAmount:          10,
					ValidDuration:      time.Second * time.Duration(10),
					BucketDuration:     time.Second * time.Duration(0),
					RateLimitAlgorithm: config.FIXED_WINDOW,
				},
			},
			instance: quota.Instance{
				Name:       "fixed_window_best_effort",
				Dimensions: map[string]interface{}{},
			},
			request: []RequestInfo{
				{true, 3, 1, 3, 10, "test"},
				{true, 3, 2, 3, 9, ""},
				{true, 3, 3, 3, 8, ""},
				{true, 3, 4, 1, 7, ""},
				{true, 3, 5, 0, 0, ""},
				{true, 3, 6, 3, 5, "test"},
				{true, 3, 10, 0, 0, ""},
				{true, 3, 11, 3, 10, ""},
				{true, 8, 12, 7, 9, ""},
				{true, 3, 13, 0, 0, ""},
				{true, 3, 14, 0, 0, ""},
				{true, 12, 21, 10, 10, "test"},
				{true, 1, 22, 0, 0, ""},
			},
		},
		"algorithm = fixed window, best effort = false": {
			quotaType: map[string]*quota.Type{
				"fixed_window": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name:               "fixed_window",
					MaxAmount:          10,
					ValidDuration:      time.Second * time.Duration(10),
					BucketDuration:     time.Second * time.Duration(0),
					RateLimitAlgorithm: config.FIXED_WINDOW,
				},
			},
			instance: quota.Instance{
				Name:       "fixed_window",
				Dimensions: map[string]interface{}{},
			},
			request: []RequestInfo{
				{false, 3, 1, 3, 10, ""},
				{false, 3, 2, 3, 9, ""},
				{false, 3, 3, 3, 8, ""},
				{false, 3, 4, 0, 0, ""},
				{false, 1, 5, 1, 6, ""},
				{false, 3, 6, 0, 0, ""},
				{false, 3, 10, 0, 0, ""},
				{false, 3, 11, 3, 10, ""},
				{false, 8, 12, 0, 0, ""},
				{false, 7, 13, 7, 8, ""},
				{false, 3, 14, 0, 0, ""},
				{false, 10, 30, 10, 10, ""},
				{false, 10, 31, 0, 0, ""},
			},
		},
		"algorithm = rolling window, best effort = true": {
			quotaType: map[string]*quota.Type{
				"rolling_window_best_effort": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name:               "rolling_window_best_effort",
					MaxAmount:          10,
					ValidDuration:      time.Second * time.Duration(100),
					BucketDuration:     time.Second * time.Duration(10),
					RateLimitAlgorithm: config.ROLLING_WINDOW,
				},
			},
			instance: quota.Instance{
				Name:       "rolling_window_best_effort",
				Dimensions: map[string]interface{}{},
			},
			request: []RequestInfo{
				{true, 3, 0, 3, 100, "test"}, // record current response
				{true, 3, 1, 3, 100, ""},
				{true, 3, 5, 3, 95, "test"}, // from the previous response
				{true, 3, 10, 3, 100, ""},
				{true, 3, 11, 1, 100, ""},
				{true, 3, 12, 0, 0, ""},
				{true, 3, 20, 0, 0, ""},
				{true, 3, 85, 3, 15, "test"}, // from the previous response
				{true, 3, 100, 3, 100, ""},
				{true, 2, 101, 2, 100, "test"}, // record current response
				{true, 2, 102, 1, 100, ""},
				{true, 1, 110, 1, 100, ""},
				{true, 1, 111, 1, 100, ""},
				{true, 1, 112, 1, 100, ""},
				{true, 1, 113, 1, 100, ""},
				{true, 1, 114, 0, 0, ""},
				{true, 1, 150, 0, 0, ""},
				{true, 10, 200, 6, 100, ""},
			},
		},
		"algorithm = rolling window, best effort = false": {
			quotaType: map[string]*quota.Type{
				"rolling_window": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name:               "rolling_window",
					MaxAmount:          10,
					ValidDuration:      time.Second * time.Duration(100),
					BucketDuration:     time.Second * time.Duration(10),
					RateLimitAlgorithm: config.ROLLING_WINDOW,
				},
			},
			instance: quota.Instance{
				Name:       "rolling_window",
				Dimensions: map[string]interface{}{},
			},
			request: []RequestInfo{
				{false, 3, 0, 3, 100, ""},
				{false, 3, 1, 3, 100, ""},
				{false, 3, 10, 3, 100, ""},
				{false, 3, 11, 0, 0, ""},
				{false, 3, 12, 0, 0, ""},
				{false, 3, 20, 0, 0, ""},
				{false, 3, 100, 3, 100, ""},
				{false, 2, 101, 2, 100, ""},
				{false, 2, 102, 2, 100, ""},
				{false, 1, 110, 1, 100, ""},
				{false, 1, 111, 1, 100, ""},
				{false, 1, 112, 1, 100, ""},
				{false, 1, 113, 0, 0, ""},
				{false, 1, 114, 0, 0, ""},
				{false, 1, 150, 0, 0, ""},
				{false, 7, 200, 7, 100, ""},
			},
		},
		"algorithm = rolling window, best effort = false, limit override": {
			quotaType: map[string]*quota.Type{
				"rolling_window_override": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name:               "rolling_window_override",
					MaxAmount:          10,
					ValidDuration:      time.Second * time.Duration(100),
					BucketDuration:     time.Second * time.Duration(10),
					RateLimitAlgorithm: config.ROLLING_WINDOW,
					Overrides: []*config.Params_Override{
						{
							MaxAmount: 50,
							Dimensions: map[string]string{
								"destination": "test",
							},
						},
						{
							MaxAmount: 5,
							Dimensions: map[string]string{
								"source": "none",
							},
						},
					},
				},
			},
			instance: quota.Instance{
				Name: "rolling_window_override",
				Dimensions: map[string]interface{}{
					"source": "none",
				},
			},
			request: []RequestInfo{
				{false, 3, 0, 3, 100, ""},
				{false, 3, 1, 0, 0, ""},
				{false, 2, 2, 2, 100, ""},
				{false, 1, 1, 0, 0, ""},
			},
		},
	}

	for id, c := range cases {
		t.Logf("Executing test case '%s'", id)
		b := info.NewBuilder().(*builder)

		b.SetAdapterConfig(&config.Params{
			Quotas:             c.quotaConfig,
			RedisServerUrl:     mockRedis.Addr(),
			ConnectionPoolSize: 10,
		})

		adapterHandler, err := b.Build(context.Background(), test.NewEnv(t))
		if err != nil {
			t.Fatalf("Got error %v, expecting success", err)
		}

		quotaHandler := adapterHandler.(*handler)

		now := time.Now()

		for _, req := range c.request {
			quotaHandler.getTime = func() time.Time {
				return now.Add(time.Duration(req.timestamp) * time.Second)
			}

			qr, err := quotaHandler.HandleQuota(context.Background(), &c.instance, adapter.QuotaArgs{
				QuotaAmount:     req.token,
				BestEffort:      req.bestEffort,
				DeduplicationID: req.deduplicationid,
			})
			if err != nil {
				t.Errorf("%v: Unexpected error %v", id, err)
			}

			if qr.ValidDuration != time.Duration(req.validDuration)*time.Second {
				t.Errorf("%v: Expecting valid duration %d, got %d at %v", id, req.validDuration, qr.ValidDuration, req.timestamp)
			}

			if qr.Amount != req.allocated {
				t.Errorf("%v: Expecting token %d, got %d at %v", id, req.allocated, qr.Amount, req.timestamp)
			}
		}
	}
}

func TestHandleQuotaErrorMsg(t *testing.T) {
	cases := map[string]struct {
		quotaType   map[string]*quota.Type
		mockRedis   map[string]server.Cmd
		quotaConfig []config.Params_Quota
		instance    quota.Instance
		req         RequestInfo
		errMsg      []string
		status      rpc.Status
	}{
		"Failed to run the script": {
			mockRedis: map[string]server.Cmd{
				"PING": func(c *server.Peer, _ string, _ []string) {
					c.WriteInline("PONG")
				},
				"SCRIPT": func(c *server.Peer, _ string, _ []string) {
					c.WriteInline("OK")
				},
				"EVALSHA": func(c *server.Peer, _ string, _ []string) {
					c.WriteError("Error")
				},
			},
			quotaType: map[string]*quota.Type{
				"fixed-window": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name:               "fixed-window",
					MaxAmount:          10,
					ValidDuration:      time.Second * 10,
					RateLimitAlgorithm: config.FIXED_WINDOW,
				},
			},
			req: RequestInfo{
				token:      3,
				bestEffort: true,
			},
			instance: quota.Instance{
				Name: "fixed-window",
				Dimensions: map[string]interface{}{
					"source": "test",
				},
			},
			errMsg: []string{
				"key: fixed-window;source=test maxAmount: 10",
				"failed to run quota script: Error",
			},
			status: status.WithUnavailable("failed to run quota script: Error"),
		},
		"Invalid response from the script": {
			mockRedis: map[string]server.Cmd{
				"PING": func(c *server.Peer, _ string, _ []string) {
					c.WriteInline("PONG")
				},
				"SCRIPT": func(c *server.Peer, _ string, _ []string) {
					c.WriteOK()
				},
				"EVALSHA": func(c *server.Peer, _ string, _ []string) {
					c.WriteLen(1)
					c.WriteInt(10)
				},
			},
			quotaType: map[string]*quota.Type{
				"fixed-window": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name:               "fixed-window",
					MaxAmount:          10,
					ValidDuration:      time.Second * 10,
					RateLimitAlgorithm: config.FIXED_WINDOW,
				},
			},
			req: RequestInfo{
				token:      3,
				bestEffort: true,
			},
			instance: quota.Instance{
				Name: "fixed-window",
				Dimensions: map[string]interface{}{
					"source": "test",
				},
			},
			errMsg: []string{
				"key: fixed-window;source=test maxAmount: 10",
				"invalid response from the redis server: [10]",
			},
			status: status.WithInternal("invalid response from the redis server: [10]"),
		},
	}

	info := GetInfo()

	for id, c := range cases {
		s, err := server.NewServer(":0")
		if err != nil {
			t.Fatal(err)
		}

		for funcTime, funcHandler := range c.mockRedis {
			if err = s.Register(funcTime, funcHandler); err != nil {
				t.Fatal(err)
			}
		}

		b := info.NewBuilder().(*builder)
		b.SetAdapterConfig(&config.Params{
			Quotas:             c.quotaConfig,
			RedisServerUrl:     s.Addr().String(),
			ConnectionPoolSize: 1,
		})
		b.SetQuotaTypes(c.quotaType)

		env := test.NewEnv(t)
		adapterHandler, err := b.Build(context.Background(), env)
		if err != nil {
			t.Errorf("%v: Unexpected error: %v", id, err.Error())
		}

		quotaHandler := adapterHandler.(*handler)

		quotaResult, err := quotaHandler.HandleQuota(context.Background(), &c.instance, adapter.QuotaArgs{
			QuotaAmount: c.req.token,
			BestEffort:  c.req.bestEffort,
		})

		if err != nil {
			t.Errorf("%v: unexpected error: %v", id, err.Error())
			continue
		}

		if c.status.GetCode() != quotaResult.Status.GetCode() {
			t.Errorf("%v: unexpected error code: %v, expected: %v", id, quotaResult.Status.GetCode(), c.status.GetCode())
			continue
		}

		if len(env.GetLogs()) != len(c.errMsg) {
			t.Errorf("%v: Invalid number of error messages. Expected: %v Got %v",
				id, len(c.errMsg), len(env.GetLogs()))
		} else {
			for idx, msg := range c.errMsg {
				if msg != env.GetLogs()[idx] {
					t.Errorf("%v: Unexpected error message. Expected: %v Got %v",
						id, msg, env.GetLogs()[idx])
				}
			}
		}

		s.Close()
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
