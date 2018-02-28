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

#include "src/envoy/http/mixer/check_data.h"
#include "common/common/base64.h"
#include "src/envoy/http/jwt_auth/jwt.h"
#include "src/envoy/http/jwt_auth/jwt_authenticator.h"
#include "src/envoy/utils/utils.h"

using HttpCheckData = ::istio::control::http::CheckData;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {
// The HTTP header to forward Istio attributes.
const LowerCaseString kIstioAttributeHeader("x-istio-attributes");

// Referer header
const LowerCaseString kRefererHeaderKey("referer");

// Set of headers excluded from request.headers attribute.
const std::set<std::string> RequestHeaderExclusives = {
    kIstioAttributeHeader.get(),
};

}  // namespace

CheckData::CheckData(const HeaderMap& headers,
                     const Network::Connection* connection)
    : headers_(headers), connection_(connection) {
  if (headers_.Path()) {
    query_params_ = Utility::parseQueryString(std::string(
        headers_.Path()->value().c_str(), headers_.Path()->value().size()));
  }
}

const LowerCaseString& CheckData::IstioAttributeHeader() {
  return kIstioAttributeHeader;
}

bool CheckData::ExtractIstioAttributes(std::string* data) const {
  // Extract attributes from x-istio-attributes header
  const HeaderEntry* entry = headers_.get(kIstioAttributeHeader);
  if (entry) {
    *data = Base64::decode(
        std::string(entry->value().c_str(), entry->value().size()));
    return true;
  }
  return false;
}

bool CheckData::GetSourceIpPort(std::string* ip, int* port) const {
  if (connection_) {
    return Utils::GetIpPort(connection_->remoteAddress()->ip(), ip, port);
  }
  return false;
}

bool CheckData::GetSourceUser(std::string* user) const {
  return Utils::GetSourceUser(connection_, user);
}

std::map<std::string, std::string> CheckData::GetRequestHeaders() const {
  return Utils::ExtractHeaders(headers_, RequestHeaderExclusives);
}

bool CheckData::IsMutualTLS() const { return Utils::IsMutualTLS(connection_); }

bool CheckData::FindHeaderByType(HttpCheckData::HeaderType header_type,
                                 std::string* value) const {
  switch (header_type) {
    case HttpCheckData::HEADER_PATH:
      if (headers_.Path()) {
        *value = std::string(headers_.Path()->value().c_str(),
                             headers_.Path()->value().size());
        return true;
      }
      break;
    case HttpCheckData::HEADER_HOST:
      if (headers_.Host()) {
        *value = std::string(headers_.Host()->value().c_str(),
                             headers_.Host()->value().size());
        return true;
      }
      break;
    case HttpCheckData::HEADER_SCHEME:
      if (headers_.Scheme()) {
        *value = std::string(headers_.Scheme()->value().c_str(),
                             headers_.Scheme()->value().size());
        return true;
      }
      break;
    case HttpCheckData::HEADER_USER_AGENT:
      if (headers_.UserAgent()) {
        *value = std::string(headers_.UserAgent()->value().c_str(),
                             headers_.UserAgent()->value().size());
        return true;
      }
      break;
    case HttpCheckData::HEADER_METHOD:
      if (headers_.Method()) {
        *value = std::string(headers_.Method()->value().c_str(),
                             headers_.Method()->value().size());
        return true;
      }
      break;
    case HttpCheckData::HEADER_REFERER: {
      const HeaderEntry* referer = headers_.get(kRefererHeaderKey);
      if (referer) {
        *value = std::string(referer->value().c_str(), referer->value().size());
        return true;
      }
    } break;
  }
  return false;
}

bool CheckData::FindHeaderByName(const std::string& name,
                                 std::string* value) const {
  const HeaderEntry* entry = headers_.get(LowerCaseString(name));
  if (entry) {
    *value = std::string(entry->value().c_str(), entry->value().size());
    return true;
  }
  return false;
}

bool CheckData::FindQueryParameter(const std::string& name,
                                   std::string* value) const {
  const auto& it = query_params_.find(name);
  if (it != query_params_.end()) {
    *value = it->second;
    return true;
  }
  return false;
}

bool CheckData::FindCookie(const std::string& name, std::string* value) const {
  std::string cookie = Utility::parseCookieValue(headers_, name);
  if (cookie != "") {
    *value = cookie;
    return true;
  }
  return false;
}

bool CheckData::GetJWTPayload(
    std::map<std::string, std::string>* payload) const {
  const HeaderEntry* entry =
      headers_.get(JwtAuth::JwtAuthenticator::JwtPayloadKey());
  if (!entry) {
    return false;
  }
  std::string value(entry->value().c_str(), entry->value().size());
  std::string payload_str = JwtAuth::Base64UrlDecode(value);
  // Return an empty string if Base64 decode fails.
  if (payload_str.empty()) {
    ENVOY_LOG(error, "Invalid {} header, invalid base64: {}",
              JwtAuth::JwtAuthenticator::JwtPayloadKey().get(), value);
    return false;
  }
  try {
    auto json_obj = Json::Factory::loadFromString(payload_str);
    json_obj->iterate(
        [payload](const std::string& key, const Json::Object& obj) -> bool {
          // will throw execption if value type is not string.
          try {
            (*payload)[key] = obj.asString();
          } catch (...) {
          }
          return true;
        });
  } catch (...) {
    ENVOY_LOG(error, "Invalid {} header, invalid json: {}",
              JwtAuth::JwtAuthenticator::JwtPayloadKey().get(), payload_str);
    return false;
  }
  return true;
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
