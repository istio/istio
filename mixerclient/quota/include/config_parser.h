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

#ifndef QUOTA_CONFIG_PARSER_H_
#define QUOTA_CONFIG_PARSER_H_

#include <memory>
#include <vector>

#include "mixer/v1/attributes.pb.h"
#include "mixer/v1/config/client/quota.pb.h"
#include "requirement.h"

namespace istio {
namespace quota {

// An object to parse quota config to generate quota requirements.
class ConfigParser {
 public:
  virtual ~ConfigParser() {}

  // Get quota requirements for a attribute set.
  virtual std::vector<Requirement> GetRequirements(
      const ::istio::mixer::v1::Attributes& attributes) const = 0;

  // The factory function to create a new instance of the parser.
  static std::unique_ptr<ConfigParser> Create(
      const ::istio::mixer::v1::config::client::QuotaSpec& spec_pb);
};

}  // namespace quota
}  // namespace istio

#endif  // QUOTA_CONFIG_PARSER_H_
