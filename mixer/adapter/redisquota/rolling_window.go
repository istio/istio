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

package redisquota

const (
	// LUA script for rolling-window algorithm
	luaRollingWindow = `
local key_meta = KEYS[1]
local key_data = KEYS[2]

local credit = tonumber(ARGV[1])
local windowLength = tonumber(ARGV[2])
local bucketLength = tonumber(ARGV[3])
local bestEffort = tonumber(ARGV[4])
local token = tonumber(ARGV[5])
local timestamp = tonumber(ARGV[6])
local deduplicationid = ARGV[7]

-- lookup previous response for the deduplicationid and returns if it is still valid
--------------------------------------------------------------------------------
if (deduplicationid or '') ~= '' then
	local previous_token = tonumber(redis.call("HGET", deduplicationid .. "-" .. key_meta, "token"))
	local previous_expire = tonumber(redis.call("HGET", deduplicationid .. "-" .. key_meta, "expire"))

	if previous_token and previous_expire then
		if timestamp < previous_expire then
			return {previous_token, previous_expire - timestamp}
		end
	end
end

-- read meta information
--------------------------------------------------------------------------------
local info_token = tonumber(redis.call("HGET", key_meta, "token"))
local info_bucket_token = tonumber(redis.call("HGET", key_meta, "bucket.token"))
local info_bucket_timestamp = tonumber(redis.call("HGET", key_meta, "bucket.timestamp"))

-- initialize meta
--------------------------------------------------------------------------------
if not info_token or not info_bucket_token or not info_bucket_timestamp then
  info_token = 0
  info_bucket_token = 0
  info_bucket_timestamp = timestamp

  redis.call("HMSET", key_meta,
    "token", info_token,
    "bucket.token", info_bucket_token,
    "bucket.timestamp", info_bucket_timestamp,
    "key", 0)
end


-- move buffer to bucket list if bucket timer is older than bucket window
--------------------------------------------------------------------------------
if (timestamp - info_bucket_timestamp + 1) > bucketLength then
  if tonumber(info_bucket_token) > 0 then
    local nextKey = redis.call("HINCRBY", key_meta, "key", 1)
    local value = tostring(nextKey) .. "." .. tostring(info_bucket_token)
    redis.call("ZADD", key_data, info_bucket_timestamp, value);
  end
  redis.call("HMSET", key_meta,
    "bucket.token", 0,
    "bucket.timestamp", timestamp)
end

local time_to_expire = timestamp - windowLength

		-- reclaim tokens from expired records
--------------------------------------------------------------------------------
local reclaimed = 0
local expired = redis.call("ZRANGEBYSCORE", key_data, 0, time_to_expire)

for idx, value in ipairs(expired) do
  reclaimed = reclaimed + tonumber(string.sub(value, string.find(value, "%.")+1))
end

-- remove expired records
--------------------------------------------------------------------------------
redis.call("ZREMRANGEBYSCORE", key_data, 0, time_to_expire)

-- update consumed token
--------------------------------------------------------------------------------
if reclaimed > 0 then
  info_token = info_token - reclaimed;
  if info_token < 0 then
    info_token = 0
  end
  redis.call("HSET", key_meta, "token", info_token)
end

-- update the expiration time for automatic cleanup
--------------------------------------------------------------------------------
redis.call("PEXPIRE", key_meta, windowLength / 1000000)
redis.call("PEXPIRE", key_meta, windowLength / 1000000)

-- calculate available token
--------------------------------------------------------------------------------
local available_token = credit - info_token

-- check available token and requested token
--------------------------------------------------------------------------------

if available_token <= 0 then
  -- credit exhausted
  return {0, 0}
elseif available_token >= token then
  -- increase token and bucket.token by token
  redis.call("HINCRBY", key_meta, "token", token)
  redis.call("HINCRBY", key_meta, "bucket.token", token)

	-- save current request and set expiration time for auto cleanup
	if (deduplicationid or '') ~= '' then
		redis.call("HMSET", deduplicationid .. "-" .. key_meta, "token", token, "expire", timestamp + windowLength)
		redis.call("PEXPIRE", deduplicationid .. "-" .. key_meta, windowLength / 1000000)
	end

  return {token,  windowLength}
else

  if bestEffort == 0 then
    -- not enough token
    return {0, 0}
  end

  -- allocate available token only
  redis.call("HINCRBY", key_meta, "token", available_token)
  redis.call("HINCRBY", key_meta, "bucket.token", available_token)

	-- save current request and set expiration time for auto cleanup
	if (deduplicationid or '') ~= '' then
		redis.call("HMSET", deduplicationid .. "-" .. key_meta, "token", available_token, "expire", timestamp + windowLength)
		redis.call("PEXPIRE", deduplicationid .. "-" .. key_meta, windowLength / 1000000)
	end

  return {available_token, windowLength}
end
`
)
