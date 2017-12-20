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
#include "google/protobuf/stubs/status.h"
#include "google/protobuf/util/json_util.h"
#include "include/attributes_builder.h"

using ::google::protobuf::Message;
using ::google::protobuf::util::Status;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer_client::AttributesBuilder;
using ::istio::mixer::v1::config::client::ServiceConfig;
using ::istio::mixer::v1::config::client::TransportConfig;

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

const std::string kV2Config("v2");

void ReadStringMap(const Json::Object& json, const std::string& name,
                   Attributes* attributes) {
  if (json.hasObject(name)) {
    json.getObject(name)->iterate(
        [attributes](const std::string& key, const Json::Object& obj) -> bool {
          AttributesBuilder(attributes).AddIpOrString(key, obj.asString());
          return true;
        });
  }
}

void ReadTransportConfig(const Json::Object& json, TransportConfig* config) {
  // Default is open, unless it specifically set to "close"
  config->set_network_fail_policy(TransportConfig::FAIL_OPEN);
  if (json.hasObject(kNetworkFailPolicy) &&
      json.getString(kNetworkFailPolicy) == "close") {
    config->set_network_fail_policy(TransportConfig::FAIL_CLOSE);
  }

  config->set_disable_check_cache(json.getBoolean(kDisableCheckCache, false));
  config->set_disable_quota_cache(json.getBoolean(kDisableQuotaCache, false));
  config->set_disable_report_batch(json.getBoolean(kDisableReportBatch, false));
}

bool ReadV2Config(const Json::Object& json, Message* message) {
  if (!json.hasObject(kV2Config)) {
    return false;
  }
  std::string v2_str = json.getObject(kV2Config)->asJsonString();
  Status status =
      ::google::protobuf::util::JsonStringToMessage(v2_str, message);
  auto& logger = Logger::Registry::getLog(Logger::Id::config);
  if (status.ok()) {
    ENVOY_LOG_TO_LOGGER(logger, info, "V2 mixer client config: {}",
                        message->DebugString());
    return true;
  }
  ENVOY_LOG_TO_LOGGER(
      logger, error,
      "Failed to convert mixer V2 client config, error: {}, data: {}",
      status.ToString(), v2_str);
  return false;
}

}  // namespace

void HttpMixerConfig::Load(const Json::Object& json) {
  ReadStringMap(json, kMixerAttributes, http_config.mutable_mixer_attributes());
  ReadStringMap(json, kForwardAttributes,
                http_config.mutable_forward_attributes());

  if (json.hasObject(kQuotaName)) {
    int64_t amount = 1;
    if (json.hasObject(kQuotaAmount)) {
      amount = std::stoi(json.getString(kQuotaAmount));
    }
    legacy_quotas.push_back({json.getString(kQuotaName), amount});
  }

  ReadTransportConfig(json, http_config.mutable_transport());

  has_v2_config = ReadV2Config(json, &http_config);
  if (has_v2_config) {
    // If v2 config is valid, clear v1 legacy_quotas.
    legacy_quotas.clear();
  }
}

void HttpMixerConfig::CreateLegacyRouteConfig(
    bool disable_check, bool disable_report,
    const std::map<std::string, std::string>& attributes,
    ServiceConfig* config) {
  config->set_disable_check_calls(disable_check);
  config->set_disable_report_calls(disable_report);

  AttributesBuilder builder(config->mutable_mixer_attributes());
  for (const auto& it : attributes) {
    builder.AddIpOrString(it.first, it.second);
  }
}

void TcpMixerConfig::Load(const Json::Object& json) {
  ReadStringMap(json, kMixerAttributes, tcp_config.mutable_mixer_attributes());

  ReadTransportConfig(json, tcp_config.mutable_transport());

  tcp_config.set_disable_check_calls(
      json.getBoolean(kDisableTcpCheckCalls, false));

  ReadV2Config(json, &tcp_config);
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
