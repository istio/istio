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

#pragma once

#include "common/common/base64.h"
#include "common/common/logger.h"
#include "envoy/http/header_map.h"

#include "include/istio/control/http/controller.h"

namespace Envoy {
namespace Utils {

namespace {
// The HTTP header to forward Istio attributes.
const Http::LowerCaseString kIstioAttributeHeader("x-istio-attributes");
};  // namespace

class HeaderUpdate : public ::istio::control::http::HeaderUpdate,
                     public Logger::Loggable<Logger::Id::filter> {
  Http::HeaderMap* headers_;

 public:
  HeaderUpdate(Http::HeaderMap* headers) : headers_(headers) {}

  void RemoveIstioAttributes() override {
    headers_->remove(kIstioAttributeHeader);
  }

  // base64 encode data, and add it to the HTTP header.
  void AddIstioAttributes(const std::string& data) override {
    std::string base64 = Base64::encode(data.c_str(), data.size());
    ENVOY_LOG(debug, "Mixer forward attributes set: {}", base64);
    headers_->setReferenceKey(kIstioAttributeHeader, base64);
  }

  static const Http::LowerCaseString& IstioAttributeHeader() {
    return kIstioAttributeHeader;
  }
};

}  // namespace Utils
}  // namespace Envoy
