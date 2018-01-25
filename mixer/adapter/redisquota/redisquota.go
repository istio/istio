// Copyright 2018 Istio Authors.
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
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/go-redis/redis"

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

	// the default window duration is 60 seconds
	defaultWindowDuration = time.Minute

	// the default bucket size is 100ms
	defaultBucketDuration = time.Millisecond * time.Duration(100)
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

///////////////// Configuration Methods ///////////////

func (b *builder) SetQuotaTypes(quotaTypes map[string]*quota.Type) {
	b.quotaTypes = quotaTypes
}

func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adapterConfig = cfg.(*config.Params)
}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	info := GetInfo()

	if len(b.adapterConfig.RedisServerUrl) == 0 {
		ce = ce.Appendf(info.Name, "redis_server_url should not be empty")
	}

	if b.adapterConfig.ConnectionPoolSize < 0 {
		ce = ce.Appendf(info.Name, "connection_pool_size of %v is invalid, must be > 0",
			b.adapterConfig.ConnectionPoolSize)
	}

	if len(b.adapterConfig.Quotas) == 0 {
		ce = ce.Appendf(info.Name, "quota should not be empty")
	}

	limits := make(map[string]*config.Params_Quota, len(b.adapterConfig.Quotas))
	for idx := range b.adapterConfig.Quotas {
		if len(b.adapterConfig.Quotas[idx].Name) == 0 {
			ce = ce.Appendf(info.Name, "quotas.name should not be empty")
			continue
		}

		limits[b.adapterConfig.Quotas[idx].Name] = &b.adapterConfig.Quotas[idx]

		if b.adapterConfig.Quotas[idx].ValidDuration == 0 {
			ce = ce.Appendf(info.Name, "quotas.valid_duration should be bigger must be > 0")
			continue
		}

		if b.adapterConfig.Quotas[idx].RateLimitAlgorithm == config.ROLLING_WINDOW {
			if b.adapterConfig.Quotas[idx].BucketDuration == 0 {
				ce = ce.Appendf(info.Name, "quotas.bucket_duration should be > 0 for ROLLING_WINDOW algorithm")
				continue
			}

			if b.adapterConfig.Quotas[idx].ValidDuration > 0 && b.adapterConfig.Quotas[idx].BucketDuration > 0 &&
				b.adapterConfig.Quotas[idx].ValidDuration <= b.adapterConfig.Quotas[idx].BucketDuration {
				ce = ce.Appendf(info.Name, "quotas.valid_duration: %v should be longer than quotas.bucket_duration: %v for ROLLING_WINDOW algorithm",
					b.adapterConfig.Quotas[idx].ValidDuration, b.adapterConfig.Quotas[idx].BucketDuration)
				continue
			}
		}

		for index := range b.adapterConfig.Quotas[idx].Overrides {
			if b.adapterConfig.Quotas[idx].Overrides[index].MaxAmount <= 0 {
				ce = ce.Appendf(info.Name, "quotas.overrides.max_amount must be > 0")
				continue
			}

			if len(b.adapterConfig.Quotas[idx].Overrides[index].Dimensions) == 0 {
				ce = ce.Appendf(info.Name, "quotas.overrides.dimensions is empty")
				continue
			}

			if _, err := getDimensionHash(b.adapterConfig.Quotas[idx].Overrides[index].Dimensions); err != nil {
				ce = ce.Appendf(info.Name, "unable to initialize quota overrides dimensions",
					b.adapterConfig.Quotas[idx].Overrides[index].Dimensions)
			}
		}
	}

	for k := range b.quotaTypes {
		if _, ok := limits[k]; !ok {
			ce = ce.Appendf(info.Name, "did not find limit defined for quota %v", k)
		}
	}

	//if b.adapterConfig.Quotas

	return
}

// getOverrideHash returns hash key of the given dimension in sorted by key
func getDimensionHash(dimensions map[string]string) (string, error) {
	var keys []string
	for k := range dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := fnv.New32a()
	for _, key := range keys {
		if _, err := io.WriteString(h, key+"\t"+dimensions[key]+"\n"); err != nil {
			return "", err
		}
	}
	return strconv.Itoa(int(h.Sum32())), nil
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	limits := make(map[string]*config.Params_Quota, len(b.adapterConfig.Quotas))
	for idx := range b.adapterConfig.Quotas {
		l := b.adapterConfig.Quotas[idx]
		limits[l.Name] = &l
	}

	// Build memory address of dimensions to hash map
	dimensionHash := make(map[*map[string]string]string)
	for key := range limits {
		for index := range limits[key].Overrides {
			if hash, err := getDimensionHash(limits[key].Overrides[index].Dimensions); err == nil {
				dimensionHash[&(limits[key].Overrides[index].Dimensions)] = hash
			}
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
		return nil, env.Logger().Errorf("could not create a connection pool with redis: %v", err)
	}

	// load scripts into redis
	scripts := make(map[config.Params_QuotaAlgorithm]*redis.Script, 2)
	for algorithm, script := range rateLimitingLUAScripts {
		scripts[algorithm] = redis.NewScript(script)
		if _, err := scripts[algorithm].Load(client).Result(); err != nil {
			return nil, env.Logger().Errorf("unable to load the LUA script: %v", err)
		}
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
		rval := (*inst)[k]
		if rval == val { // this dimension matches, on to next comparison.
			continue
		}

		// if rval has a string representation then compare it with val
		// For example net.ip has a useful string representation.
		switch v := rval.(type) {
		case fmt.Stringer:
			if v.String() == val {
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
	key := quota.Name

	for idx := range quota.Overrides {
		if matchDimensions(&quota.Overrides[idx].Dimensions, &instance.Dimensions) {
			if h.logger.VerbosityLevel(4) {
				h.logger.Infof("quota override: %v selected for %v", quota.Overrides[idx], *instance)
			}

			if hash, ok := h.dimensionHash[&quota.Overrides[idx].Dimensions]; ok {
				// override key and max amount
				key = key + "-" + hash
				maxAmount = quota.Overrides[idx].MaxAmount
			} else {
				// This should not be happen
				return "", 0, fmt.Errorf("quota override dimension hash lookup failed: %v in %v",
					h.limits[instance.Name].Overrides[idx].Dimensions, h.dimensionHash)
			}
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
				return adapter.QuotaResult{}, nil
			}

			if h.logger.VerbosityLevel(4) {
				h.logger.Infof("key: %v maxAmount: %v", key, maxAmount)
			}

			// execute lua algorithm script
			result, err := script.Run(
				h.client,
				[]string{
					key + ".meta", // KEY[1]
					key + ".data", // KEY[2]
				},
				maxAmount, // ARGV[1] credit
				limit.GetValidDuration().Nanoseconds(),  // ARGV[2] window length
				limit.GetBucketDuration().Nanoseconds(), // ARGV[3] bucket length
				args.BestEffort,                         // ARGV[4] best effort
				args.QuotaAmount,                        // ARGV[5] token
				now.UnixNano(),                          // ARGV[6] timestamp
				args.DeduplicationID,                    // ARGS[8] deduplication id
			).Result()

			if err != nil {
				_ = h.logger.Errorf("failed to run quota script: %v", err)
				return adapter.QuotaResult{}, nil
			}

			allocated, expiration, err := getAllocatedTokenFromResult(&result)
			if err != nil {
				_ = h.logger.Errorf("%v", err)
				return adapter.QuotaResult{}, nil
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

	return adapter.QuotaResult{}, nil
}

func (h handler) Close() error {
	return h.client.Close()
}

////////////////// Bootstrap //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "redisquota",
		Impl:        "istio.io/mixer/adapter/redisquota",
		Description: "Redis-based quotas.",
		SupportedTemplates: []string{
			quota.TemplateName,
		},
		DefaultConfig: &config.Params{
			RedisServerUrl:     "localhost:6379",
			ConnectionPoolSize: 10,
		},
		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
	}
}

///////////////////////////////////////////////////////
