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

#include "src/signature.h"
#include "utils/md5.h"

using std::string;
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;

namespace istio {
namespace mixer_client {
namespace {
const char kDelimiter[] = "\0";
const int kDelimiterLength = 1;
}  // namespace

string GenerateSignature(const Attributes& attributes,
                         const CacheKeySet& cache_keys) {
  MD5 hasher;

  for (const auto& attribute : attributes.attributes) {
    const SubKeySet* sub_keys = cache_keys.Find(attribute.first);
    // Skip the attributes not in the cache keys
    if (sub_keys == nullptr) {
      continue;
    }
    hasher.Update(attribute.first);
    hasher.Update(kDelimiter, kDelimiterLength);
    switch (attribute.second.type) {
      case Attributes::Value::ValueType::STRING:
        hasher.Update(attribute.second.str_v);
        break;
      case Attributes::Value::ValueType::BYTES:
        hasher.Update(attribute.second.str_v);
        break;
      case Attributes::Value::ValueType::INT64:
        hasher.Update(&attribute.second.value.int64_v,
                      sizeof(attribute.second.value.int64_v));
        break;
      case Attributes::Value::ValueType::DOUBLE:
        hasher.Update(&attribute.second.value.double_v,
                      sizeof(attribute.second.value.double_v));
        break;
      case Attributes::Value::ValueType::BOOL:
        hasher.Update(&attribute.second.value.bool_v,
                      sizeof(attribute.second.value.bool_v));
        break;
      case Attributes::Value::ValueType::TIME:
        hasher.Update(&attribute.second.time_v,
                      sizeof(attribute.second.time_v));
        break;
      case Attributes::Value::ValueType::DURATION:
        hasher.Update(&attribute.second.duration_nanos_v,
                      sizeof(attribute.second.duration_nanos_v));
        break;
      case Attributes::Value::ValueType::STRING_MAP:
        for (const auto& it : attribute.second.string_map_v) {
          if (sub_keys->Found(it.first)) {
            hasher.Update(it.first);
            hasher.Update(kDelimiter, kDelimiterLength);
            hasher.Update(it.second);
            hasher.Update(kDelimiter, kDelimiterLength);
          }
        }
        break;
    }
    hasher.Update(kDelimiter, kDelimiterLength);
  }

  return hasher.Digest();
}

}  // namespace mixer_client
}  // namespace istio
