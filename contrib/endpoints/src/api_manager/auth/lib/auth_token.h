/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
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
