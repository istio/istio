/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_AUTH_LIB_GRPC_INTERNALS_H_
#define API_MANAGER_AUTH_LIB_GRPC_INTERNALS_H_

// This header file is for internal use only since it declares grpc
// internals that auth depends on. A public header file should not
// include any internal grpc header files.

// TODO: Remove this dependency on gRPC internal implementation details,
// or work with gRPC team to support this functionality as a public API
// surface.

extern "C" {

#include "src/core/lib/json/json.h"
#include "src/core/lib/json/json_common.h"
#include "src/core/lib/security/credentials/jwt/json_token.h"
#include "src/core/lib/security/credentials/jwt/jwt_verifier.h"
#include "src/core/lib/slice/b64.h"
#include "src/core/lib/support/string.h"
}

#endif  // API_MANAGER_AUTH_LIB_GRPC_INTERNALS_H_
