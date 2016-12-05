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
#include "contrib/endpoints/src/api_manager/auth/lib/json_util.h"
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
