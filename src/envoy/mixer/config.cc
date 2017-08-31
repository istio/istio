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

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// The Json object name for static attributes.
const std::string kMixerAttributes("mixer_attributes");

// The Json object name to specify attributes which will be forwarded
// to the upstream istio proxy.
const std::string kForwardAttributes("forward_attributes");

// The Json object name for quota name and amount.
const std::string kQuotaName("quota_name");
const std::string kQuotaAmount("quota_amount");

// The Json object name to disable check cache, quota cache and report batch
const std::string kDisableCheckCache("disable_check_cache");
const std::string kDisableQuotaCache("disable_quota_cache");
const std::string kDisableReportBatch("disable_report_batch");

const std::string kNetworkFailPolicy("network_fail_policy");
const std::string kDisableTcpCheckCalls("disable_tcp_check_calls");

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

}  // namespace

void MixerConfig::Load(const Json::Object& json) {
  ReadStringMap(json, kMixerAttributes, &mixer_attributes);
  ReadStringMap(json, kForwardAttributes, &forward_attributes);

  ReadString(json, kQuotaName, &quota_name);
  ReadString(json, kQuotaAmount, &quota_amount);

  ReadString(json, kNetworkFailPolicy, &network_fail_policy);

  disable_check_cache = json.getBoolean(kDisableCheckCache, false);
  disable_quota_cache = json.getBoolean(kDisableQuotaCache, false);
  disable_report_batch = json.getBoolean(kDisableReportBatch, false);

  disable_tcp_check_calls = json.getBoolean(kDisableTcpCheckCalls, false);
}

void MixerConfig::ExtractQuotaAttributes(Attributes* attr) const {
  if (!quota_name.empty()) {
    attr->attributes[Attributes::kQuotaName] =
        Attributes::StringValue(quota_name);

    int64_t amount = 1;  // default amount to 1.
    if (!quota_amount.empty()) {
      amount = std::stoi(quota_amount);
    }
    attr->attributes[Attributes::kQuotaAmount] = Attributes::Int64Value(amount);
  }
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
