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
#include "contrib/endpoints/src/api_manager/auth/lib/base64.h"

#include <cstring>
#include <string>

#include "contrib/endpoints/src/api_manager/auth/lib/grpc_internals.h"

namespace google {
namespace api_manager {
namespace auth {

char *esp_base64_encode(const void *data, size_t data_size, bool url_safe,
                        bool multiline, bool padding) {
  char *result =
      grpc_base64_encode(data, data_size, url_safe ? 1 : 0, multiline ? 1 : 0);
  if (result == nullptr) {
    return result;
  }
  // grpc_base64_encode may have added padding. If not needed, remove them.
  if (!padding) {
    size_t len = strlen(result);
    while (len > 0 && result[len - 1] == '=') {
      len--;
    }
    result[len] = '\0';
  }
  return result;
}

}  // namespace auth
}  // namespace api_manager
}  // namespace google
