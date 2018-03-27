/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#include "src/envoy/utils/authn.h"
#include "common/common/base64.h"
#include "src/istio/authn/context.pb.h"

using istio::authn::Result;

namespace Envoy {
namespace Utils {
namespace {

// The HTTP header to save authentication result.
const Http::LowerCaseString kAuthenticationOutputHeaderLocation(
    "sec-istio-authn-payload");
}  // namespace

bool Authentication::SaveResultToHeader(const istio::authn::Result& result,
                                        Http::HeaderMap* headers) {
  if (HasResultInHeader(*headers)) {
    ENVOY_LOG(warn,
              "Authentication result already exist in header. Cannot save");
    return false;
  }

  std::string payload_data;
  result.SerializeToString(&payload_data);
  headers->addCopy(kAuthenticationOutputHeaderLocation,
                   Base64::encode(payload_data.c_str(), payload_data.size()));
  return true;
}

bool Authentication::FetchResultFromHeader(const Http::HeaderMap& headers,
                                           istio::authn::Result* result) {
  const auto entry = headers.get(kAuthenticationOutputHeaderLocation);
  if (entry == nullptr) {
    return false;
  }
  std::string value(entry->value().c_str(), entry->value().size());
  return result->ParseFromString(Base64::decode(value));
}

void Authentication::ClearResultInHeader(Http::HeaderMap* headers) {
  headers->remove(kAuthenticationOutputHeaderLocation);
}

bool Authentication::HasResultInHeader(const Http::HeaderMap& headers) {
  return headers.get(kAuthenticationOutputHeaderLocation) != nullptr;
}

const Http::LowerCaseString& Authentication::GetHeaderLocation() {
  return kAuthenticationOutputHeaderLocation;
}

}  // namespace Utils
}  // namespace Envoy
