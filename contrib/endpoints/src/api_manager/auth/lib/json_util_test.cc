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
#include "src/api_manager/auth/lib/json_util.h"
#include "gtest/gtest.h"

#include <string.h>

namespace google {
namespace api_manager {
namespace auth {
namespace {

const char json_input[] =
    "{"
    "  \"string\": \"string value\","
    "  \"number\": 12345,"
    "  \"null\": null,"
    "  \"true\": true,"
    "  \"false\": false,"
    "  \"object\": { },"
    "  \"array\": [ ],"
    "}";

TEST(JsonUtil, GetPropertyValue) {
  char *json_copy = strdup(json_input);
  grpc_json *json =
      grpc_json_parse_string_with_len(json_copy, strlen(json_copy));

  const char *string_value = GetStringValue(json, "string");
  ASSERT_STREQ("string value", string_value);

  const char *number_value = GetNumberValue(json, "number");
  ASSERT_STREQ("12345", number_value);

  grpc_json_destroy(json);
  free(json_copy);
}

TEST(JsonUtil, GetProperty) {
  char *json_copy = strdup(json_input);
  grpc_json *json =
      grpc_json_parse_string_with_len(json_copy, strlen(json_copy));

  const grpc_json *json_property;

  json_property = GetProperty(json, "string");
  ASSERT_NE(nullptr, json_property);
  ASSERT_STREQ("string", json_property->key);
  ASSERT_STREQ("string value", json_property->value);
  ASSERT_EQ(GRPC_JSON_STRING, json_property->type);

  json_property = GetProperty(json, "number");
  ASSERT_NE(nullptr, json_property);
  ASSERT_STREQ("number", json_property->key);
  ASSERT_STREQ("12345", json_property->value);
  ASSERT_EQ(GRPC_JSON_NUMBER, json_property->type);

  json_property = GetProperty(json, "null");
  ASSERT_NE(nullptr, json_property);
  ASSERT_STREQ("null", json_property->key);
  ASSERT_EQ(nullptr, json_property->value);
  ASSERT_EQ(GRPC_JSON_NULL, json_property->type);

  json_property = GetProperty(json, "true");
  ASSERT_NE(nullptr, json_property);
  ASSERT_STREQ("true", json_property->key);
  ASSERT_EQ(nullptr, json_property->value);
  ASSERT_EQ(GRPC_JSON_TRUE, json_property->type);

  json_property = GetProperty(json, "false");
  ASSERT_NE(nullptr, json_property);
  ASSERT_STREQ("false", json_property->key);
  ASSERT_EQ(nullptr, json_property->value);
  ASSERT_EQ(GRPC_JSON_FALSE, json_property->type);

  json_property = GetProperty(json, "string");
  ASSERT_NE(nullptr, json_property);
  ASSERT_STREQ("string", json_property->key);
  ASSERT_STREQ("string value", json_property->value);
  ASSERT_EQ(GRPC_JSON_STRING, json_property->type);

  json_property = GetProperty(json, "object");
  ASSERT_NE(nullptr, json_property);
  ASSERT_STREQ("object", json_property->key);
  ASSERT_EQ(GRPC_JSON_OBJECT, json_property->type);

  json_property = GetProperty(json, "array");
  ASSERT_NE(nullptr, json_property);
  ASSERT_STREQ("array", json_property->key);
  ASSERT_EQ(GRPC_JSON_ARRAY, json_property->type);

  grpc_json_destroy(json);
  free(json_copy);
}

}  // namespace
}  // namespace auth
}  // namespace api_manager
}  // namespace google
