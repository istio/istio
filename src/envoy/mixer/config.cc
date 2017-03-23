/* Copyright 2017 Istio Authors. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include "src/envoy/mixer/config.h"

using ::istio::mixer_client::Attributes;

namespace Http {
namespace Mixer {
namespace {

// The Json object name for mixer-server.
const std::string kJsonNameMixerServer("mixer_server");

// The Json object name for static attributes.
const std::string kJsonNameMixerAttributes("mixer_attributes");

// The Json object name to specify attributes which will be forwarded
// to the upstream istio proxy.
const std::string kJsonNameForwardAttributes("forward_attributes");

// The Json object name for quota name and amount.
const std::string kJsonNameQuotaName("quota_name");
const std::string kJsonNameQuotaAmount("quota_amount");

// The Json object name for check cache keys.
const std::string kJsonNameCheckCacheKeys("check_cache_keys");

void ReadString(const Json::Object& json, const std::string& name,
                std::string* value) {
  if (json.hasObject(name)) {
    *value = json.getString(name);
  }
}

void ReadStringMap(const Json::Object& json, const std::string& name,
                   std::map<std::string, std::string>* map) {
  if (json.hasObject(name)) {
    json.getObject(name)->iterate(
        [map](const std::string& key, const Json::Object& obj) -> bool {
          (*map)[key] = obj.asString();
          return true;
        });
  }
}

void ReadStringVector(const Json::Object& json, const std::string& name,
                      std::vector<std::string>* value) {
  if (json.hasObject(name)) {
    auto v = json.getStringArray(name);
    value->swap(v);
  }
}

}  // namespace

void MixerConfig::Load(const Json::Object& json) {
  ReadString(json, kJsonNameMixerServer, &mixer_server);

  ReadStringMap(json, kJsonNameMixerAttributes, &mixer_attributes);
  ReadStringMap(json, kJsonNameForwardAttributes, &forward_attributes);

  ReadString(json, kJsonNameQuotaName, &quota_name);
  ReadString(json, kJsonNameQuotaAmount, &quota_amount);

  ReadStringVector(json, kJsonNameCheckCacheKeys, &check_cache_keys);
}

void MixerConfig::ExtractQuotaAttributes(Attributes* attr) const {
  if (!quota_name.empty()) {
    attr->attributes[ ::istio::mixer_client::kQuotaName] =
        Attributes::StringValue(quota_name);

    int64_t amount = 1;  // default amount to 1.
    if (!quota_amount.empty()) {
      amount = std::stoi(quota_amount);
    }
    attr->attributes[ ::istio::mixer_client::kQuotaAmount] =
        Attributes::Int64Value(amount);
  }
}

}  // namespace Mixer
}  // namespace Http
