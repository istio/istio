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
#include "src/api_manager/auth/lib/base64.h"

#include <cstring>
#include <string>

#include "src/api_manager/auth/lib/grpc_internals.h"

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
