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
#ifndef API_MANAGER_AUTH_LIB_GRPC_INTERNALS_H_
#define API_MANAGER_AUTH_LIB_GRPC_INTERNALS_H_

// This header file contains definitions for all grpc internal
// dependencies. The code that depends on grpc internals should
// include this file instead of including the original headers
// in grpc.

// This header file is for internal use only since it declares grpc
// internals that auth depends on. A public header file should not
// include any internal grpc header files.

// TODO: Remove this dependency on gRPC internal implementation details,
// or work with gRPC team to support this functionality as a public API
// surface.

extern "C" {

#include <grpc/support/slice.h>
#include <openssl/rsa.h>

//////////////////////////////////////////////////////
// definitions from grpc/src/core/json/json_common.h
//////////////////////////////////////////////////////

/* The various json types. */
typedef enum {
  GRPC_JSON_OBJECT,
  GRPC_JSON_ARRAY,
  GRPC_JSON_STRING,
  GRPC_JSON_NUMBER,
  GRPC_JSON_TRUE,
  GRPC_JSON_FALSE,
  GRPC_JSON_NULL,
  GRPC_JSON_TOP_LEVEL
} grpc_json_type;

//////////////////////////////////////////////////////
// definitions from grpc/src/core/json/json.h
//////////////////////////////////////////////////////

/* A tree-like structure to hold json values. The key and value pointers
 * are not owned by it.
 */
typedef struct grpc_json {
  struct grpc_json *next;
  struct grpc_json *prev;
  struct grpc_json *child;
  struct grpc_json *parent;

  grpc_json_type type;
  const char *key;
  const char *value;
} grpc_json;

/* The next two functions are going to parse the input string, and
 * modify it in the process, in order to use its space to store
 * all of the keys and values for the returned object tree.
 *
 * They assume UTF-8 input stream, and will output UTF-8 encoded
 * strings in the tree. The input stream's UTF-8 isn't validated,
 * as in, what you input is what you get as an output.
 *
 * All the keys and values in the grpc_json objects will be strings
 * pointing at your input buffer.
 *
 * Delete the allocated tree afterward using grpc_json_destroy().
 */
grpc_json *grpc_json_parse_string_with_len(char *input, size_t size);
grpc_json *grpc_json_parse_string(char *input);

/* Use these to create or delete a grpc_json object.
 * Deletion is recursive. We will not attempt to free any of the strings
 * in any of the objects of that tree.
 */
grpc_json *grpc_json_create(grpc_json_type type);
void grpc_json_destroy(grpc_json *json);

/* This function will create a new string using gpr_realloc, and will
 * deserialize the grpc_json tree into it. It'll be zero-terminated,
 * but will be allocated in chunks of 256 bytes.
 *
 * The indent parameter controls the way the output is formatted.
 * If indent is 0, then newlines will be suppressed as well, and the
 * output will be condensed at its maximum.
 */
char *grpc_json_dump_to_string(grpc_json *json, int indent);

//////////////////////////////////////////////////////
// definitions from grpc/src/core/security/base64
//////////////////////////////////////////////////////

/* Encodes data using base64. It is the caller's responsability to free
   the returned char * using gpr_free. Returns nullptr on nullptr input. */
char *grpc_base64_encode(const void *data, size_t data_size, int url_safe,
                         int multiline);

/* Decodes data according to the base64 specification. Returns an empty
   slice in case of failure. */
gpr_slice grpc_base64_decode(const char *b64, int url_safe);

/* Same as above except that the length is provided by the caller. */
gpr_slice grpc_base64_decode_with_len(const char *b64, size_t b64_len,
                                      int url_safe);

//////////////////////////////////////////////////////
// definitions from grpc/src/core/auth/security/json_key.h
//////////////////////////////////////////////////////

/* --- auth_json_key parsing. --- */

typedef struct {
  const char *type;
  char *private_key_id;
  char *client_id;
  char *client_email;
  RSA *private_key;
} grpc_auth_json_key;

/* Creates a json_key object from string. Returns an invalid object if a parsing
   error has been encountered. */
grpc_auth_json_key grpc_auth_json_key_create_from_string(
    const char *json_string);

/* Destructs the object. */
void grpc_auth_json_key_destruct(grpc_auth_json_key *json_key);

/* Caller is responsible for calling gpr_free on the returned value. May return
   nullptr on invalid input. The scope parameter may be nullptr. */
char *grpc_jwt_encode_and_sign(const grpc_auth_json_key *json_key,
                               const char *audience,
                               gpr_timespec token_lifetime, const char *scope);

//////////////////////////////////////////////////////
// definitions from grpc/src/core/support/string.h
//////////////////////////////////////////////////////

/* Minimum buffer size for calling ltoa */
#define GPR_LTOA_MIN_BUFSIZE (3 * sizeof(long))

/* Convert a long to a string in base 10; returns the length of the
   output string (or 0 on failure).
   output must be at least GPR_LTOA_MIN_BUFSIZE bytes long. */
int gpr_ltoa(long value, char *output);

//////////////////////////////////////////////////////
// definitions from grpc/src/core/security/jwt_verifier.h
//////////////////////////////////////////////////////

/* --- grpc_jwt_verifier_status. --- */

typedef enum {
  GRPC_JWT_VERIFIER_OK = 0,
  GRPC_JWT_VERIFIER_BAD_SIGNATURE,
  GRPC_JWT_VERIFIER_BAD_FORMAT,
  GRPC_JWT_VERIFIER_BAD_AUDIENCE,
  GRPC_JWT_VERIFIER_KEY_RETRIEVAL_ERROR,
  GRPC_JWT_VERIFIER_TIME_CONSTRAINT_FAILURE,
  GRPC_JWT_VERIFIER_GENERIC_ERROR
} grpc_jwt_verifier_status;

const char *grpc_jwt_verifier_status_to_string(grpc_jwt_verifier_status status);

/* --- grpc_jwt_claims. --- */

typedef struct grpc_jwt_claims grpc_jwt_claims;

void grpc_jwt_claims_destroy(grpc_jwt_claims *claims);

/* Returns the whole JSON tree of the claims. */
const grpc_json *grpc_jwt_claims_json(const grpc_jwt_claims *claims);

/* Access to registered claims in https://tools.ietf.org/html/rfc7519#page-9 */
const char *grpc_jwt_claims_subject(const grpc_jwt_claims *claims);
const char *grpc_jwt_claims_issuer(const grpc_jwt_claims *claims);
const char *grpc_jwt_claims_audience(const grpc_jwt_claims *claims);
gpr_timespec grpc_jwt_claims_expires_at(const grpc_jwt_claims *claims);

/* --- TESTING ONLY exposed functions. --- */

grpc_jwt_claims *grpc_jwt_claims_from_json(grpc_json *json, gpr_slice buffer);
grpc_jwt_verifier_status grpc_jwt_claims_check(const grpc_jwt_claims *claims,
                                               const char *audience);
}

#endif  // API_MANAGER_AUTH_LIB_GRPC_INTERNALS_H_
