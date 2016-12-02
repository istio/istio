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
#ifndef API_MANAGER_AUTH_LIB_AUTH_TOKEN_H_
#define API_MANAGER_AUTH_LIB_AUTH_TOKEN_H_

#include <stddef.h>

namespace google {
namespace api_manager {
namespace auth {

// Parse a json secret and generate auth_token
// Returned pointer need to be freed by esp_grpc_free
char *esp_get_auth_token(const char *json_secret, const char *audience);

// Free a buffer allocated by gRPC library.
void esp_grpc_free(char *token);

// Parses a JSON service account auth token in the following format:
// {
//   "access_token":" ... ",
//   "expires_in":100,
//   "token_type":"Bearer"
// }
// Returns true on success, false otherwise. On success, *token is set to the
// malloc'd auth token (value of 'access_token' JSON property) and *expires is
// set to the value of 'expires_in' property (token expiration in seconds.
bool esp_get_service_account_auth_token(char *input, size_t size, char **token,
                                        int *expires);

}  // namespace auth
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_AUTH_LIB_AUTH_TOKEN_H_
