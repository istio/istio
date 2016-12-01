// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
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
