// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/api_manager/auth/lib/json.h"

#include <cstring>
#include <string>

#include "src/api_manager/auth/lib/json_util.h"

namespace google {
namespace api_manager {
namespace auth {

char *WriteUserInfoToJson(const UserInfo &user_info) {
  grpc_json json_top;
  memset(&json_top, 0, sizeof(json_top));
  json_top.type = GRPC_JSON_OBJECT;

  grpc_json json_issuer;
  FillChild(&json_issuer, nullptr, &json_top, "issuer",
            user_info.issuer.c_str(), GRPC_JSON_STRING);

  grpc_json json_id;
  FillChild(&json_id, &json_issuer, &json_top, "id", user_info.id.c_str(),
            GRPC_JSON_STRING);

  grpc_json json_email;
  FillChild(&json_email, &json_id, &json_top, "email", user_info.email.c_str(),
            GRPC_JSON_STRING);

  grpc_json json_consumer_id;
  FillChild(&json_consumer_id, &json_email, &json_top, "consumer_id",
            user_info.consumer_id.c_str(), GRPC_JSON_STRING);

  return grpc_json_dump_to_string(&json_top, 0);
}

}  // namespace auth
}  // namespace api_manager
}  // namespace google
