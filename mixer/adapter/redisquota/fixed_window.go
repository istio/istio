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
	// LUA script for fixed window
	luaFixedWindow = `
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

-- read or initialize meta information
--------------------------------------------------------------------------------
local info_token = tonumber(redis.call("HGET", key_meta, "token"))
local info_expire = tonumber(redis.call("HGET", key_meta, "expire"))

if (not info_token or not info_expire) or (timestamp >= info_expire) then
  info_token = 0
  info_expire = windowLength + timestamp

  redis.call("HMSET", key_meta, "token", info_token, "expire", windowLength + timestamp)
	-- set the expiration time for automatic cleanup
	redis.call("PEXPIRE", key_meta, windowLength / 1000000)
end

if info_token + token > credit then
  if bestEffort == 1 then
    local exceeded = info_token + token - credit

    if exceeded < token then
      -- return maximum available allocated token
      redis.call("HMSET", key_meta, "token", credit)

    	-- save current request and set expiration time for auto cleanup
			if (deduplicationid or '') ~= '' then
		    redis.call("HMSET", deduplicationid .. "-" .. key_meta, "token", token - exceeded, "expire", info_expire)
			  redis.call("PEXPIRE", deduplicationid .. "-" .. key_meta, math.floor((info_expire - timestamp) / 1000000))
		  end

      return {token - exceeded, info_expire - timestamp}
    else
      -- not enough available credit
      return {0, 0}
    end
  else
    -- not enough available credit
    return {0, 0}
  end
else
  -- allocated token
  redis.call("HMSET", key_meta, "token", info_token + token)

	-- save current request and set expiration time for auto cleanup
	if (deduplicationid or '') ~= '' then
		redis.call("HMSET", deduplicationid .. "-" .. key_meta, "token", token, "expire", info_expire)
		redis.call("PEXPIRE", deduplicationid .. "-" .. key_meta, math.floor((info_expire - timestamp) / 1000000))
	end

  return {token, info_expire - timestamp}
end
`
)
