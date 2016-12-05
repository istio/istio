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
#include "contrib/endpoints/src/api_manager/method_impl.h"
#include "gtest/gtest.h"

using std::string;

namespace google {
namespace api_manager {

namespace {

const char kMethodName[] = "method";
const char kIssuer1[] = "iss1";
const char kIssuer2[] = "iss2";
const char kIssuer2https[] = "https://iss2";
const char kIssuer3[] = "iss3";
const char kIssuer3http[] = "http://iss3/";
const char kIssuer4[] = "iss4";

TEST(MethodInfo, Create) {
  MethodInfoImplPtr method_info(new MethodInfoImpl(kMethodName, "", ""));
  ASSERT_FALSE(method_info->auth());
  ASSERT_FALSE(method_info->allow_unregistered_calls());
  ASSERT_EQ(kMethodName, method_info->name());

  method_info->set_auth(true);
  ASSERT_TRUE(method_info->auth());

  method_info->set_allow_unregistered_calls(true);
  ASSERT_TRUE(method_info->allow_unregistered_calls());
}

TEST(MethodInfo, IssueAndAudiences) {
  MethodInfoImplPtr method_info(new MethodInfoImpl(kMethodName, "", ""));
  method_info->addAudiencesForIssuer(kIssuer1, "aud1,aud2");
  method_info->addAudiencesForIssuer(kIssuer1, "aud3");
  method_info->addAudiencesForIssuer(kIssuer1, ",");
  method_info->addAudiencesForIssuer(kIssuer1, ",aud4");
  method_info->addAudiencesForIssuer(kIssuer1, "");
  method_info->addAudiencesForIssuer(kIssuer1, ",aud5,,,");
  method_info->addAudiencesForIssuer(kIssuer2https, ",,,aud6");
  method_info->addAudiencesForIssuer(kIssuer3http, "");
  method_info->addAudiencesForIssuer(kIssuer3http, "https://aud7");
  method_info->addAudiencesForIssuer(kIssuer3http, "http://aud8");
  method_info->addAudiencesForIssuer(kIssuer3http, "https://aud9/");

  ASSERT_TRUE(method_info->isIssuerAllowed(kIssuer1));
  ASSERT_TRUE(method_info->isIssuerAllowed(kIssuer2));
  ASSERT_TRUE(method_info->isIssuerAllowed(kIssuer3));
  ASSERT_FALSE(method_info->isIssuerAllowed(kIssuer4));

  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer1, {"aud1"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer1, {"aud1", "audx"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer1, {"aud2"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer1, {"aud2", "audx"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer1, {"aud1", "aud2"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer1, {"aud3"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer1, {"aud4"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer1, {"aud5"}));
  ASSERT_FALSE(method_info->isAudienceAllowed(kIssuer1, {"aud6"}));

  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer2, {"aud6"}));
  ASSERT_FALSE(method_info->isAudienceAllowed(kIssuer2, {"aud1"}));

  ASSERT_FALSE(method_info->isAudienceAllowed(kIssuer3, {"aud1"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer3, {"aud7"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer3, {"aud8"}));
  ASSERT_TRUE(method_info->isAudienceAllowed(kIssuer3, {"aud9"}));

  // some negative test cases
  ASSERT_FALSE(method_info->isAudienceAllowed("", {"aud1"}));
  ASSERT_FALSE(method_info->isAudienceAllowed(kIssuer1, {""}));
  ASSERT_FALSE(method_info->isAudienceAllowed(kIssuer1, {}));
}

TEST(MethodInfo, TestParameters) {
  MethodInfoImplPtr method_info(new MethodInfoImpl(kMethodName, "", ""));

  ASSERT_EQ(nullptr, method_info->url_query_parameters("xyz"));
  ASSERT_EQ(nullptr, method_info->http_header_parameters("xyz"));

  method_info->add_http_header_parameter("name1", "Http-Header1");
  method_info->add_http_header_parameter("name1", "Http-Header2");
  ASSERT_EQ(nullptr, method_info->http_header_parameters("name2"));
  auto http_headers = method_info->http_header_parameters("name1");
  ASSERT_NE(nullptr, http_headers);
  ASSERT_EQ(2ul, http_headers->size());
  ASSERT_EQ((*http_headers)[0], "Http-Header1");
  ASSERT_EQ((*http_headers)[1], "Http-Header2");

  method_info->add_url_query_parameter("name1", "url_query1");
  method_info->add_url_query_parameter("name1", "url_query2");
  ASSERT_EQ(nullptr, method_info->url_query_parameters("name2"));
  auto url_queries = method_info->url_query_parameters("name1");
  ASSERT_NE(nullptr, url_queries);
  ASSERT_EQ(2ul, url_queries->size());
  ASSERT_EQ((*url_queries)[0], "url_query1");
  ASSERT_EQ((*url_queries)[1], "url_query2");
}

TEST(MethodInfo, PreservesBackendAddress) {
  MethodInfoImplPtr method_info(new MethodInfoImpl(kMethodName, "", ""));
  method_info->set_backend_address("backend");
  ASSERT_EQ(method_info->backend_address(), "backend");
}

}  // namespace

}  // namespace api_manager
}  // namespace google
