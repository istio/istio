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

#include "src/envoy/mixer/quota_config.h"
#include <regex>

using ::istio::mixer_client::Attributes;
using ::istio::mixer::v1::config::client::AttributeMatch;
using ::istio::mixer::v1::config::client::QuotaSpec;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

bool MatchAttributes(const AttributeMatch& match,
                     const Attributes& attributes) {
  for (const auto& map_it : match.clause()) {
    // map is attribute_name to StringMatch.
    const std::string& name = map_it.first;
    const auto& match = map_it.second;

    // Check if required attribure exists with string type.
    const auto& it = attributes.attributes.find(name);
    if (it == attributes.attributes.end() ||
        it->second.type != Attributes::Value::STRING) {
      return false;
    }
    const std::string& value = it->second.str_v;

    switch (match.match_type_case()) {
      case ::istio::proxy::v1::config::StringMatch::kExact:
        if (value != match.exact()) {
          return false;
        }
        break;
      case ::istio::proxy::v1::config::StringMatch::kPrefix:
        if (value.length() < match.prefix().length() ||
            value.compare(0, match.prefix().length(), match.prefix()) != 0) {
          return false;
        }
        break;
      case ::istio::proxy::v1::config::StringMatch::kRegex:
        // TODO: re-use std::regex object for optimization
        if (!std::regex_match(value, std::regex(match.regex()))) {
          return false;
        }
        break;
      default:
        // match_type not set case, an empty StringMatch, ignore it.
        break;
    }
  }
  return true;
}

}  // namespace

QuotaConfig::QuotaConfig(const QuotaSpec& spec_pb) : spec_pb_(spec_pb) {}

std::vector<QuotaConfig::Quota> QuotaConfig::Check(
    const Attributes& attributes) const {
  std::vector<Quota> results;
  for (const auto& rule : spec_pb_.rules()) {
    bool matched = false;
    for (const auto& match : rule.match()) {
      if (MatchAttributes(match, attributes)) {
        matched = true;
        break;
      }
    }
    // If not match, applies to all requests.
    if (matched || rule.match_size() == 0) {
      for (const auto& quota : rule.quotas()) {
        results.push_back({quota.quota(), quota.charge()});
      }
    }
  }
  return results;
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
