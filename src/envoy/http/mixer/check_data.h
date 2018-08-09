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

#include "common/common/logger.h"
#include "common/http/utility.h"
#include "envoy/api/v2/core/base.pb.h"
#include "envoy/http/header_map.h"
#include "google/protobuf/struct.pb.h"
#include "include/istio/control/http/controller.h"
#include "src/istio/authn/context.pb.h"

namespace Envoy {
namespace Http {
namespace Mixer {

class CheckData : public ::istio::control::http::CheckData,
                  public Logger::Loggable<Logger::Id::filter> {
 public:
  CheckData(const HeaderMap& headers,
            const envoy::api::v2::core::Metadata& metadata,
            const Network::Connection* connection);

  // Find "x-istio-attributes" headers, if found base64 decode
  // its value and remove it from the headers.
  bool ExtractIstioAttributes(std::string* data) const override;

  bool GetSourceIpPort(std::string* ip, int* port) const override;

  bool GetPrincipal(bool peer, std::string* user) const override;

  std::map<std::string, std::string> GetRequestHeaders() const override;

  bool IsMutualTLS() const override;

  bool GetRequestedServerName(std::string* name) const override;

  bool FindHeaderByType(
      ::istio::control::http::CheckData::HeaderType header_type,
      std::string* value) const override;

  bool FindHeaderByName(const std::string& name,
                        std::string* value) const override;

  bool FindQueryParameter(const std::string& name,
                          std::string* value) const override;

  bool FindCookie(const std::string& name, std::string* value) const override;

  const ::google::protobuf::Struct* GetAuthenticationResult() const override;

 private:
  const HeaderMap& headers_;
  const envoy::api::v2::core::Metadata& metadata_;
  const Network::Connection* connection_;
  Utility::QueryParams query_params_;
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
