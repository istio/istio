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

#include "config_parser_impl.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::Attributes_AttributeValue;
using ::istio::mixer::v1::config::client::AttributeMatch;
using ::istio::mixer::v1::config::client::StringMatch;
using ::istio::mixer::v1::config::client::QuotaRule;
using ::istio::mixer::v1::config::client::QuotaSpec;

namespace istio {
namespace quota {

ConfigParserImpl::ConfigParserImpl(const QuotaSpec& spec_pb)
    : spec_pb_(spec_pb) {
  // Build regex map
  for (const auto& rule : spec_pb_.rules()) {
    for (const auto& match : rule.match()) {
      for (const auto& map_it : match.clause()) {
        const auto& match = map_it.second;
        if (match.match_type_case() == StringMatch::kRegex) {
          regex_map_[match.regex()] = std::regex(match.regex());
        }
      }
    }
  }
}

std::vector<Requirement> ConfigParserImpl::GetRequirements(
    const Attributes& attributes) const {
  std::vector<Requirement> results;
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

bool ConfigParserImpl::MatchAttributes(const AttributeMatch& match,
                                       const Attributes& attributes) const {
  const auto& attributes_map = attributes.attributes();
  for (const auto& map_it : match.clause()) {
    // map is attribute_name to StringMatch.
    const std::string& name = map_it.first;
    const auto& match = map_it.second;

    // Check if required attribure exists with string type.
    const auto& it = attributes_map.find(name);
    if (it == attributes_map.end() ||
        it->second.value_case() != Attributes_AttributeValue::kStringValue) {
      return false;
    }
    const std::string& value = it->second.string_value();

    switch (match.match_type_case()) {
      case StringMatch::kExact:
        if (value != match.exact()) {
          return false;
        }
        break;
      case StringMatch::kPrefix:
        if (value.length() < match.prefix().length() ||
            value.compare(0, match.prefix().length(), match.prefix()) != 0) {
          return false;
        }
        break;
      case StringMatch::kRegex: {
        const auto& reg_it = regex_map_.find(match.regex());
        // All regex should be pre-build.
        GOOGLE_CHECK(reg_it != regex_map_.end());
        if (!std::regex_match(value, reg_it->second)) {
          return false;
        }
      } break;
      default:
        // match_type not set case, an empty StringMatch, ignore it.
        break;
    }
  }
  return true;
}

std::unique_ptr<ConfigParser> ConfigParser::Create(
    const ::istio::mixer::v1::config::client::QuotaSpec& spec_pb) {
  return std::unique_ptr<ConfigParser>(new ConfigParserImpl(spec_pb));
}

}  // namespace quota
}  // namespace istio
