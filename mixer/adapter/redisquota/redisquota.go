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
//
//
// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/redisquota/config/config.proto -x "-n redisquota -t quota -d example"
package redisquota // import "istio.io/istio/mixer/adapter/redisquota"

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/go-redis/redis"

	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/adapter/redisquota/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/istio/mixer/template/quota"
)

var (
	// LUA rate-limiting algorithm scripts
	rateLimitingLUAScripts = map[config.Params_QuotaAlgorithm]string{
		config.FIXED_WINDOW:   luaFixedWindow,
		config.ROLLING_WINDOW: luaRollingWindow,
	}
)

type (
	builder struct {
		quotaTypes    map[string]*quota.Type
		adapterConfig *config.Params
	}

	handler struct {
		// go-redis client
		// connection pool with redis
		client *redis.Client

		// the limits we know about
		limits map[string]*config.Params_Quota

		// dimension hash map
		dimensionHash map[*map[string]string]string

		// list of algorithm LUA scripts
		scripts map[config.Params_QuotaAlgorithm]*redis.Script

		// indirection to support fast deterministic tests
		getTime func() time.Time

		// logger provided by the framework
		logger adapter.Logger
	}
)

// ensure our types implement the requisite interfaces
var _ quota.HandlerBuilder = &builder{}
var _ quota.Handler = &handler{}

///////////////// Configuration Methods ///////////////

func (b *builder) SetQuotaTypes(quotaTypes map[string]*quota.Type) {
	b.quotaTypes = quotaTypes
}

func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adapterConfig = cfg.(*config.Params)
}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	info := GetInfo()

	if len(b.adapterConfig.Quotas) == 0 {
		ce = ce.Appendf("quotas", "quota should not be empty")
	}

	limits := make(map[string]*config.Params_Quota, len(b.adapterConfig.Quotas))
	for idx := range b.adapterConfig.Quotas {
		quotas := &b.adapterConfig.Quotas[idx]

		if len(quotas.Name) == 0 {
			ce = ce.Appendf("name", "quotas.name should not be empty")
			continue
		}

		limits[quotas.Name] = quotas

		if quotas.ValidDuration == 0 {
			ce = ce.Appendf("valid_duration", "quotas.valid_duration should be bigger must be > 0")
			continue
		}

		if quotas.RateLimitAlgorithm == config.ROLLING_WINDOW {
			if quotas.BucketDuration == 0 {
				ce = ce.Appendf("bucket_duration", "quotas.bucket_duration should be > 0 for ROLLING_WINDOW algorithm")
				continue
			}

			if quotas.ValidDuration > 0 && quotas.BucketDuration > 0 &&
				quotas.ValidDuration <= quotas.BucketDuration {
				ce = ce.Appendf("valid_duration", "quotas.valid_duration: %v should be longer than quotas.bucket_duration: %v for ROLLING_WINDOW algorithm",
					quotas.ValidDuration, quotas.BucketDuration)
				continue
			}
		}

		for index := range quotas.Overrides {
			if quotas.Overrides[index].MaxAmount <= 0 {
				ce = ce.Appendf("max_amount", "quotas.overrides.max_amount must be > 0")
				continue
			}

			if len(quotas.Overrides[index].Dimensions) == 0 {
				ce = ce.Appendf("dimensions", "quotas.overrides.dimensions is empty")
				continue
			}
		}
	}

	for k := range b.quotaTypes {
		if _, ok := limits[k]; !ok {
			ce = ce.Appendf("quotas", "did not find limit defined for quota %v", k)
		}
	}

	// check redis related configuration
	if b.adapterConfig.ConnectionPoolSize < 0 {
		ce = ce.Appendf("connection_pool_size", "connection_pool_size of %v is invalid, must be > 0",
			b.adapterConfig.ConnectionPoolSize)
	}

	if len(b.adapterConfig.RedisServerUrl) == 0 {
		ce = ce.Appendf("redis_server_url", "redis_server_url should not be empty")
	}

	// test redis connection
	option := redis.Options{
		Addr: b.adapterConfig.RedisServerUrl,
	}

	if b.adapterConfig.ConnectionPoolSize > 0 {
		option.PoolSize = int(b.adapterConfig.ConnectionPoolSize)
	}

	client := redis.NewClient(&option)
	if _, err := client.Ping().Result(); err != nil {
		ce = ce.Appendf(info.Name, "could not create a connection to redis server: %v", err)
		return
	}

	// check scripts loading to redis
	scripts := make(map[config.Params_QuotaAlgorithm]*redis.Script, 2)
	for algorithm, script := range rateLimitingLUAScripts {
		scripts[algorithm] = redis.NewScript(script)
		if _, err := scripts[algorithm].Load(client).Result(); err != nil {
			ce = ce.Appendf(info.Name, "unable to initialized redis service: %v", err)
			return
		}
	}

	_ = client.Close()
	return
}

// getOverrideHash returns hash key of the given dimension in sorted by key
func getDimensionHash(dimensions map[string]string) string {
	var keys []string
	for k := range dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := fnv.New32a()
	for _, key := range keys {
		_, _ = io.WriteString(h, key+"\t"+dimensions[key]+"\n")
	}
	return strconv.Itoa(int(h.Sum32()))
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	limits := make(map[string]*config.Params_Quota, len(b.adapterConfig.Quotas))
	for idx := range b.adapterConfig.Quotas {
		limits[b.adapterConfig.Quotas[idx].Name] = &b.adapterConfig.Quotas[idx]
	}

	// Build memory address of dimensions to hash map
	dimensionHash := make(map[*map[string]string]string)
	for key := range limits {
		for index := range limits[key].Overrides {
			dimensionHash[&(limits[key].Overrides[index].Dimensions)] =
				getDimensionHash(limits[key].Overrides[index].Dimensions)
		}
	}

	// initialize redis client
	option := redis.Options{
		Addr: b.adapterConfig.RedisServerUrl,
	}

	if b.adapterConfig.ConnectionPoolSize > 0 {
		option.PoolSize = int(b.adapterConfig.ConnectionPoolSize)
	}

	client := redis.NewClient(&option)
	if _, err := client.Ping().Result(); err != nil {
		return nil, fmt.Errorf("could not create a connection to redis server: %v", err)
	}

	// load scripts into redis
	scripts := make(map[config.Params_QuotaAlgorithm]*redis.Script, 2)
	for algorithm, script := range rateLimitingLUAScripts {
		scripts[algorithm] = redis.NewScript(script)
	}

	h := &handler{
		client:        client,
		limits:        limits,
		scripts:       scripts,
		logger:        env.Logger(),
		getTime:       time.Now,
		dimensionHash: dimensionHash,
	}

	return h, nil
}

////////////////// Runtime Methods //////////////////////////

// matchDimensions matches configured dimensions with dimensions of the instance.
func matchDimensions(cfg *map[string]string, inst *map[string]interface{}) bool {
	for k, val := range *cfg {
		if rval, ok := (*inst)[k]; ok {
			if adapter.StringEquals(rval, val) { // this dimension matches, on to next comparison.
				continue
			}
		}

		// rval does not match val.
		return false
	}
	return true
}

func getAllocatedTokenFromResult(result *interface{}) (int64, time.Duration, error) {
	if res, ok := (*result).([]interface{}); ok {
		if len(res) != 2 {
			return 0, 0, fmt.Errorf("invalid response from the redis server: %v", *result)
		}

		// read token
		tokenValue, tokenOk := res[0].(int64)
		if !tokenOk {
			return 0, 0, fmt.Errorf("invalid response from the redis server: %v", result)
		}

		// read expiration
		expValue, expOk := res[1].(int64)
		if !expOk {
			return 0, 0, fmt.Errorf("invalid response from the redis server: %v", result)
		}

		return tokenValue, time.Duration(expValue) * time.Nanosecond, nil
	}

	return 0, 0, fmt.Errorf("invalid response from the redis server: %v", result)
}

// find override
func (h *handler) getKeyAndQuotaAmount(instance *quota.Instance, quota *config.Params_Quota) (string, int64, error) {
	maxAmount := quota.MaxAmount
	key := makeKey(quota.Name, instance.Dimensions)

	for idx := range quota.Overrides {
		if matchDimensions(&quota.Overrides[idx].Dimensions, &instance.Dimensions) {
			h.logger.Debugf("quota override: %v selected for %v", quota.Overrides[idx], *instance)

			if hash, ok := h.dimensionHash[&quota.Overrides[idx].Dimensions]; ok {
				// override key and max amount
				key = key + "-" + hash
				maxAmount = quota.Overrides[idx].MaxAmount
				return key, maxAmount, nil
			}

			// This should not be happen
			return "", 0, fmt.Errorf("quota override dimension hash lookup failed: %v in %v",
				h.limits[instance.Name].Overrides[idx].Dimensions, h.dimensionHash)
		}
	}

	return key, maxAmount, nil
}

func (h *handler) HandleQuota(context context.Context, instance *quota.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	now := h.getTime()
	if limit, ok := h.limits[instance.Name]; ok {
		if script, ok := h.scripts[limit.RateLimitAlgorithm]; ok {
			ret := status.OK

			// get overridden quotaAmount and quotaKey
			key, maxAmount, err := h.getKeyAndQuotaAmount(instance, limit)
			if err != nil {
				_ = h.logger.Errorf("%v", err.Error())
				return adapter.QuotaResult{
					Status: status.WithInternal("redisquota: Internal Error"),
				}, nil
			}

			h.logger.Debugf("key: %v maxAmount: %v", key, maxAmount)

			// execute lua algorithm script
			result, err := script.Run(
				h.client,
				[]string{
					key + ".meta", // KEY[1]
					key + ".data", // KEY[2]
				},
				maxAmount,                               // ARGV[1] credit
				limit.GetValidDuration().Nanoseconds(),  // ARGV[2] window length
				limit.GetBucketDuration().Nanoseconds(), // ARGV[3] bucket length
				args.BestEffort,                         // ARGV[4] best effort
				args.QuotaAmount,                        // ARGV[5] token
				now.UnixNano(),                          // ARGV[6] timestamp
				args.DeduplicationID,                    // ARGS[8] deduplication id
			).Result()

			if err != nil {
				_ = h.logger.Errorf("failed to run quota script: %v", err)
				return adapter.QuotaResult{
					Status: status.WithUnavailable("redisquota: Service Unavailable"),
				}, nil
			}

			allocated, expiration, err := getAllocatedTokenFromResult(&result)
			if err != nil {
				_ = h.logger.Errorf("%v", err)
				return adapter.QuotaResult{
					Status: status.WithInternal("redisquota: Internal Error"),
				}, nil
			}

			if allocated <= 0 {
				ret = status.WithResourceExhausted("redisquota: Resource exhausted")
			}

			return adapter.QuotaResult{
				Status:        ret,
				Amount:        allocated,
				ValidDuration: expiration * time.Nanosecond,
			}, nil
		}
	}

	return adapter.QuotaResult{
		Status: status.OK,
	}, nil
}

func (h handler) Close() error {
	return h.client.Close()
}

////////////////// Bootstrap //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("redisquota")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}

///////////////////////////////////////////////////////
