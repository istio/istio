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

#include "authn_utils.h"
#include "common/json/json_loader.h"
#include "proxy/src/envoy/http/jwt_auth/jwt.h"

namespace Envoy {
namespace Http {
namespace Istio {
namespace AuthN {
namespace {
// The JWT audience key name
static const std::string kJwtAudienceKey = "aud";

// The JWT groups key name
static const std::string kJwtGroupsKey = "groups";

// Extract JWT audience into the JwtPayload.
// This function should to be called after the claims are extracted.
void ExtractJwtAudience(
    const Envoy::Json::Object& obj,
    const ::google::protobuf::Map< ::std::string, ::std::string>& claims,
    istio::authn::JwtPayload* payload) {
  const std::string& key = kJwtAudienceKey;
  // "aud" can be either string array or string.
  // First, try as string
  if (claims.count(key) > 0) {
    payload->add_audiences(claims.at(key));
    return;
  }
  // Next, try as string array
  try {
    std::vector<std::string> aud_vector = obj.getStringArray(key);
    for (const std::string aud : aud_vector) {
      payload->add_audiences(aud);
    }
  } catch (Json::Exception& e) {
    // Not convertable to string array
  }
}

// Extract JWT groups into the JwtPayload.
// This function should to be called after the claims are extracted.
void ExtractJwtGroups(
    const Envoy::Json::Object& obj,
    const ::google::protobuf::Map< ::std::string, ::std::string>& claims,
    istio::authn::JwtPayload* payload) {
  const std::string& key = kJwtGroupsKey;
  // "groups" can be either string array or string.
  // First, try as string
  if (claims.count(key) > 0) {
    payload->add_groups(claims.at(key));
    return;
  }
  // Next, try as string array
  try {
    std::vector<std::string> group_vector = obj.getStringArray(key);
    for (const std::string group : group_vector) {
      payload->add_groups(group);
    }
  } catch (Json::Exception& e) {
    // Not convertable to string array
  }
}
};  // namespace

bool AuthnUtils::ProcessJwtPayload(const std::string& payload_str,
                                   istio::authn::JwtPayload* payload) {
  Envoy::Json::ObjectSharedPtr json_obj;
  try {
    json_obj = Json::Factory::loadFromString(payload_str);
    ENVOY_LOG(debug, "{}: json object is {}", __FUNCTION__,
              json_obj->asJsonString());
  } catch (...) {
    return false;
  }

  *payload->mutable_raw_claims() = payload_str;
  ::google::protobuf::Map< ::std::string, ::std::string>* claims =
      payload->mutable_claims();

  // Extract claims
  json_obj->iterate(
      [payload](const std::string& key, const Json::Object& obj) -> bool {
        ::google::protobuf::Map< ::std::string, ::std::string>* claims =
            payload->mutable_claims();
        // In current implementation, only string objects are extracted into
        // claims. If call obj.asJsonString(), will get "panic: not reached"
        // from json_loader.cc.
        try {
          // Try as string, will throw execption if object type is not string.
          (*claims)[key] = obj.asString();
        } catch (Json::Exception& e) {
        }
        return true;
      });
  // Extract audience
  // ExtractJwtAudience() should be called after claims are extracted.
  ExtractJwtAudience(*json_obj, payload->claims(), payload);
  // ExtractJwtGroups() should be called after claims are extracted.
  ExtractJwtGroups(*json_obj, payload->claims(), payload);
  // Build user
  if (claims->count("iss") > 0 && claims->count("sub") > 0) {
    payload->set_user((*claims)["iss"] + "/" + (*claims)["sub"]);
  }
  // Build authorized presenter (azp)
  if (claims->count("azp") > 0) {
    payload->set_presenter((*claims)["azp"]);
  }
  return true;
}

}  // namespace AuthN
}  // namespace Istio
}  // namespace Http
}  // namespace Envoy
