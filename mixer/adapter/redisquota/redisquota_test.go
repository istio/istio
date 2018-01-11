// Copyright 2017 Istio Authors.
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
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis"
	"github.com/alicebob/miniredis/server"
	"github.com/go-redis/redis"
	"github.com/golang/glog"
	lua "github.com/yuin/gopher-lua"

	"istio.io/istio/mixer/adapter/redisquota/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/template/quota"
)

type (
	LocalRedisServer struct {
		redisServer     *server.Server
		miniredisServer *miniredis.Miniredis
		miniredisClient *redis.Client

		scriptmap map[string]string
	}

	QuotaInfo struct {
		algorithm    string
		name         string
		credit       int64
		windowLength int64
		bucketLength int64
	}

	RequestInfo struct {
		bestEffort      bool
		token           int64
		timestamp       int64
		allocated       int64
		validDuration   int64
		deduplicationid string
	}
)

func NewLocalRedisServer(addr string) (*LocalRedisServer, error) {
	mockServer := LocalRedisServer{
		redisServer:     nil,
		miniredisServer: nil,
		miniredisClient: nil,
		scriptmap:       nil,
	}

	var err error

	mockServer.scriptmap = map[string]string{}

	// initialize mini redis to handle redis command
	mockServer.miniredisServer, err = miniredis.Run()
	if err != nil {
		panic(err)
	}

	mockServer.miniredisClient = redis.NewClient(&redis.Options{
		Addr:     mockServer.miniredisServer.Addr(),
		PoolSize: 10,
	})

	// initialize mock redis to handle EVAL
	mockServer.redisServer, err = server.NewServer(addr)
	if err != nil {
		panic(err)
	}

	// register command handler to mock redis
	mockServer.redisServer.Register("PING", func(c *server.Peer, cmd string, args []string) {
		c.WriteInline("PONG")
	})

	runEvalFunc := func(c *server.Peer, script string, args []string) error {
		L := lua.NewState()
		defer L.Close()

		// set global variable KEYS
		keysTable := L.NewTable()
		keysLen, err := strconv.Atoi(args[1])
		if err != nil {
			c.WriteError(err.Error())
			return err
		}
		for i := 0; i < keysLen; i++ {
			L.RawSet(keysTable, lua.LNumber(i+1), lua.LString(args[i+2]))
		}
		L.SetGlobal("KEYS", keysTable)

		// set global variable ARGV
		argvTable := L.NewTable()
		argvLen := len(args) - 2 - keysLen
		for i := 0; i < argvLen; i++ {
			L.RawSet(argvTable, lua.LNumber(i+1), lua.LString(args[i+2+keysLen]))
		}
		L.SetGlobal("ARGV", argvTable)

		// Register call function to lua VM
		redisFuncs := map[string]lua.LGFunction{
			"call": func(L *lua.LState) int {
				top := L.GetTop()
				args := make([]interface{}, top)
				for i := 1; i <= top; i++ {
					arg := L.Get(i)

					dataType := arg.Type()
					switch dataType {
					case lua.LTBool:
						args[i-1] = lua.LVAsBool(arg)
					case lua.LTNumber:
						value, _ := strconv.ParseFloat(lua.LVAsString(arg), 64)
						args[i-1] = value
					case lua.LTString:
						args[i-1] = lua.LVAsString(arg)
					case lua.LTNil:
					case lua.LTFunction:
					case lua.LTUserData:
					case lua.LTThread:
					case lua.LTTable:
					case lua.LTChannel:
					default:
						args[i-1] = nil
					}
				}
				glog.V(3).Infof("%v", args)

				cmd := redis.NewCmd(args...)
				err := mockServer.miniredisClient.Process(cmd)
				if err != nil {
					if err.Error() != "redis: nil" {
						glog.Infof("err=[%v]", err)
					}
				}

				res, err := cmd.Result()
				glog.V(3).Infof("%v %v %v", res, reflect.TypeOf(res), err)
				if err != nil {
					if err.Error() != "redis: nil" {
						return 0
					}
				}

				pushCount := 0
				resType := reflect.TypeOf(res)
				if resType == nil {
					L.Push(lua.LNil)
					pushCount++
				} else {
					if resType.String() == "string" {
						L.Push(lua.LString(res.(string)))
						pushCount++
					} else if resType.String() == "int64" {
						L.Push(lua.LNumber(res.(int64)))
						pushCount++
					} else if resType.String() == "[]interface {}" {
						rettb := L.NewTable()
						glog.V(3).Infof("%v", rettb)
						for _, e := range res.([]interface{}) {
							if reflect.TypeOf(e).String() == "int64" {
								L.RawSet(rettb, lua.LNumber(rettb.Len()+1), lua.LNumber(e.(int64)))
							} else {
								L.RawSet(rettb, lua.LNumber(rettb.Len()+1), lua.LString(e.(string)))
							}
						}
						L.Push(rettb)
						pushCount++
						glog.V(3).Infof("%v", pushCount)

					}
				}

				glog.V(3).Infof("%v", pushCount)
				return pushCount // Notify that we pushed one value to the stack
			},
		}

		// Register command handlers
		L.Push(L.NewFunction(func(L *lua.LState) int {
			mod := L.RegisterModule("redis", redisFuncs).(*lua.LTable)
			L.Push(mod)
			return 1
		}))

		L.Push(lua.LString("redis"))
		L.Call(1, 0)

		if err := L.DoString(script); err != nil {
			c.WriteError(fmt.Sprintf("%v", err))
			return err
		}

		result := []lua.LValue{}
		for i := 1; i <= L.GetTop(); i++ {
			arg := L.Get(i)
			switch arg.Type() {
			case lua.LTTable:
				for j := 1; true; j++ {
					val := L.GetTable(arg, lua.LNumber(j))
					if val.Type() == lua.LTNil {
						break
					}
					result = append(result, val)
				}
			default:
				result = append(result, arg)
			}
		}

		c.WriteLen(len(result))
		for _, res := range result {
			switch res.Type() {
			case lua.LTNil:
				c.WriteNull()
			case lua.LTNumber:
				c.WriteInt(int(lua.LVAsNumber(res)))
			default:
				c.WriteInline(lua.LVAsString(res))
			}
		}

		return nil
	}

	// Register EVAL, EVALSHA, and SCRIPT command to miniredis
	mockServer.redisServer.Register("EVAL", func(c *server.Peer, cmd string, args []string) {
		runEvalFunc(c, args[0], args)
	})

	mockServer.redisServer.Register("EVALSHA", func(c *server.Peer, cmd string, args []string) {
		if script, ok := mockServer.scriptmap[args[0]]; ok {
			runEvalFunc(c, script, args)
		} else {
			c.WriteError(fmt.Sprintf("Invalid SHA %v", args[0]))
		}
	})

	mockServer.redisServer.Register("SCRIPT", func(c *server.Peer, cmd string, args []string) {
		sub := strings.Trim(strings.ToLower(args[0]), " \t")
		if sub == "load" {
			h := sha1.New()
			io.WriteString(h, args[1])
			hash := hex.EncodeToString(h.Sum(nil))

			mockServer.scriptmap[hash] = args[1]
			c.WriteInline(hash)
		} else if sub == "flush" {
			// not supported
		} else if sub == "kill" {
			// not supported
		} else if sub == "exists" {
			argLen := len(args) - 1
			c.WriteLen(argLen)
			for i := 1; i <= argLen; i++ {
				if _, ok := mockServer.scriptmap[args[i]]; ok {
					c.WriteInt(1)
				} else {
					c.WriteInt(0)
				}
			}
		} else {
			c.WriteError(fmt.Sprintf("Invalid script command %v", sub))
		}
	})

	return &mockServer, nil
}

func (s *LocalRedisServer) Close() {
	s.miniredisServer.Close()
	s.miniredisClient.Close()
	s.redisServer.Close()
}

func TestBuilderValidate(t *testing.T) {
	cases := map[string]struct {
		config *config.Params
		errCnt int
		errMsg []string
	}{
		"Good configuration": {
			config: &config.Params{
				RedisServerUrl:     "localhost:6476",
				ConnectionPoolSize: 10,
			},
			errCnt: 0,
			errMsg: []string{},
		},
		"Empty redis server url": {
			config: &config.Params{
				RedisServerUrl:     "",
				ConnectionPoolSize: 10,
			},
			errCnt: 1,
			errMsg: []string{
				"redisServerUrl: redis server url should not be empty",
			},
		},
		"Negative connection pool size": {
			config: &config.Params{
				RedisServerUrl:     "localhost:6476",
				ConnectionPoolSize: -1,
			},
			errCnt: 1,
			errMsg: []string{
				"connectionPoolSize: redis connection pool size of -1 is invalid, must be > 0",
			},
		},
	}

	info := GetInfo()

	for id, c := range cases {
		b := info.NewBuilder().(*builder)
		b.SetAdapterConfig(c.config)

		ce := b.Validate()
		if ce != nil {
			if len(ce.Multi.Errors) != c.errCnt {
				t.Errorf("%v: Invalid number of errors. Expected %v got %v", id, c.errCnt, len(ce.Multi.Errors))
			} else {
				for idx, err := range ce.Multi.Errors {
					if c.errMsg[idx] != err.Error() {
						t.Errorf("%v: Invalid error message. Expected %v got %v", id, c.errMsg[idx], err.Error())
					}
				}
			}
		} else if c.errCnt > 0 {
			t.Errorf("%v: Succeeded. Error expected", id)
		}
	}
}

func TestBuilderBuildErrorMsg(t *testing.T) {
	cases := map[string]struct {
		quotaType   map[string]*quota.Type
		errMsg      string
		mockRedis   map[string]interface{}
		quotaConfig []config.Params_Quota
	}{
		"Good configuration": {
			mockRedis: map[string]interface{}{
				"PING": func(c *server.Peer, cmd string, args []string) {
					c.WriteInline("PONG")
				},
				"SCRIPT": func(c *server.Peer, cmd string, args []string) {
					c.WriteInline("SUCCESS")
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
					RateLimitAlgorithm: "fixed-window",
				},
			},
			errMsg: "",
		},
		"Missing quota config": {
			mockRedis: map[string]interface{}{
				"PING": func(c *server.Peer, cmd string, args []string) {
					c.WriteInline("PONG")
				},
			},
			quotaType: map[string]*quota.Type{
				"rolling-window": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name: "fixed-window",
				},
			},
			errMsg: "did not find limit defined for quota rolling-window",
		},
		"Redis server initial check": {
			mockRedis: map[string]interface{}{
				"PING": func(c *server.Peer, cmd string, args []string) {
					c.WriteError("Error")
				},
			},
			quotaType: map[string]*quota.Type{
				"fixed-window": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name: "fixed-window",
				},
			},
			errMsg: "could not create a connection pool with redis: Error",
		},
		"LUA script loading error": {
			mockRedis: map[string]interface{}{
				"PING": func(c *server.Peer, cmd string, args []string) {
					c.WriteInline("PONG")
				},
				"SCRIPT": func(c *server.Peer, cmd string, args []string) {
					c.WriteError("Error")
				},
			},
			quotaType: map[string]*quota.Type{
				"fixed-window": {},
			},
			quotaConfig: []config.Params_Quota{
				{
					Name: "fixed-window",
				},
			},
			errMsg: "unable to load the LUA script: Error",
		},
	}

	info := GetInfo()
	env := test.NewEnv(t)

	for id, c := range cases {
		s, err := server.NewServer(":0")
		if err != nil {
			t.Fatal(err)
		}

		for funcTime, funcHandler := range c.mockRedis {
			s.Register(funcTime, funcHandler.(func(c *server.Peer, cmd string, args []string)))
		}

		b := info.NewBuilder().(*builder)
		b.SetAdapterConfig(&config.Params{
			Quotas:             c.quotaConfig,
			RedisServerUrl:     s.Addr().String(),
			ConnectionPoolSize: 1,
		})
		b.SetQuotaTypes(c.quotaType)

		_, err = b.Build(context.Background(), env)
		if err != nil {
			if c.errMsg != err.Error() {
				t.Errorf("%v: Expected error: %v, Got: %v", id, c.errMsg, err.Error())
			}
		} else if len(c.errMsg) > 0 {
			t.Errorf("%v: Succeeded. Expected error: %v", id, c.errMsg)
		}

		s.Close()
	}
}

func TestHandleQuota(t *testing.T) {
	mockRedis, err := NewLocalRedisServer("127.0.0.1:0")
	if err != nil {
		glog.Fatalf("Unable to start mock redis server: %v", err)
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
		meta        map[string]interface{}
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
					RateLimitAlgorithm: "fixed-window",
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
					RateLimitAlgorithm: "fixed-window",
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
					RateLimitAlgorithm: "rolling-window",
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
					RateLimitAlgorithm: "rolling-window",
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
					RateLimitAlgorithm: "rolling-window",
					Overrides: []config.Params_Override{
						{
							MaxAmount: 5,
							Dimensions: map[string]string{
								"source": "test",
							},
						},
					},
				},
			},
			instance: quota.Instance{
				Name: "rolling_window_override",
				Dimensions: map[string]interface{}{
					"source": "test",
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
		b := info.NewBuilder().(*builder)

		b.SetAdapterConfig(&config.Params{
			Quotas:             c.quotaConfig,
			RedisServerUrl:     mockRedis.redisServer.Addr().String(),
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
		mockRedis   map[string]interface{}
		quotaConfig []config.Params_Quota
		instance    quota.Instance
		req         RequestInfo
		errMsg      []string
	}{
		"Failed to run the script": {
			mockRedis: map[string]interface{}{
				"PING": func(c *server.Peer, cmd string, args []string) {
					c.WriteInline("PONG")
				},
				"SCRIPT": func(c *server.Peer, cmd string, args []string) {
					c.WriteInline("OK")
				},
				"EVALSHA": func(c *server.Peer, cmd string, args []string) {
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
					RateLimitAlgorithm: "fixed-window",
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
				"failed to run quota script: Error",
			},
		},
		"Invalid response from the script": {
			mockRedis: map[string]interface{}{
				"PING": func(c *server.Peer, cmd string, args []string) {
					c.WriteInline("PONG")
				},
				"SCRIPT": func(c *server.Peer, cmd string, args []string) {
					c.WriteOK()
				},
				"EVALSHA": func(c *server.Peer, cmd string, args []string) {
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
					RateLimitAlgorithm: "fixed-window",
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
				"invalid response from the redis server: [10]",
			},
		},
	}

	info := GetInfo()

	for id, c := range cases {
		s, err := server.NewServer(":0")
		if err != nil {
			t.Fatal(err)
		}

		for funcTime, funcHandler := range c.mockRedis {
			s.Register(funcTime, funcHandler.(func(c *server.Peer, cmd string, args []string)))
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

		_, err = quotaHandler.HandleQuota(context.Background(), &c.instance, adapter.QuotaArgs{
			QuotaAmount: c.req.token,
			BestEffort:  c.req.bestEffort,
		})

		glog.Errorf("%v", env.GetLogs())
		glog.Errorf("%v", c.errMsg)

		if len(env.GetLogs()) != len(c.errMsg) {
			t.Errorf("%v: Invalid number of error messages. Exptected: %v Got %v",
				id, len(c.errMsg), len(env.GetLogs()))
		} else {
			for idx, msg := range c.errMsg {
				if msg != env.GetLogs()[idx] {
					t.Errorf("%v: Unexpected error message. Exptected: %v Got %v",
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
