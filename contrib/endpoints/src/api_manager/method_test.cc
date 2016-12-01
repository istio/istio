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
#include "gtest/gtest.h"
#include "src/api_manager/method_impl.h"

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
