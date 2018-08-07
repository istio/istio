/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#include "src/envoy/utils/authn.h"
#include "common/protobuf/protobuf.h"
#include "envoy/http/header_map.h"
#include "include/istio/utils/attribute_names.h"
#include "src/istio/authn/context.pb.h"
#include "test/test_common/utility.h"

using istio::authn::Result;

namespace Envoy {
namespace Utils {

class AuthenticationTest : public testing::Test {
 protected:
  void SetUp() override {
    test_result_.set_principal("foo");
    test_result_.set_peer_user("bar");
  }

  const Http::LowerCaseString& GetHeaderLocation() {
    return Authentication::GetHeaderLocation();
  }
  Http::TestHeaderMapImpl request_headers_{};
  Result test_result_;
};

TEST_F(AuthenticationTest, SaveEmptyResult) {
  EXPECT_FALSE(Authentication::HasResultInHeader(request_headers_));
  EXPECT_TRUE(Authentication::SaveResultToHeader(Result{}, &request_headers_));
  EXPECT_TRUE(Authentication::HasResultInHeader(request_headers_));
  const auto entry = request_headers_.get(GetHeaderLocation());
  EXPECT_TRUE(entry != nullptr);
  EXPECT_EQ("", entry->value().getStringView());
}

TEST_F(AuthenticationTest, SaveSomeResult) {
  EXPECT_TRUE(
      Authentication::SaveResultToHeader(test_result_, &request_headers_));
  const auto entry = request_headers_.get(GetHeaderLocation());
  EXPECT_TRUE(entry != nullptr);
  EXPECT_EQ("CgNmb28SA2Jhcg==", entry->value().getStringView());
}

TEST_F(AuthenticationTest, SaveAuthAttributesToStruct) {
  istio::authn::Result result;
  ::google::protobuf::Struct data;

  Authentication::SaveAuthAttributesToStruct(result, data);
  EXPECT_TRUE(data.mutable_fields()->empty());

  result.set_principal("principal");
  result.set_peer_user("peeruser");
  auto origin = result.mutable_origin();
  origin->add_audiences("audiences0");
  origin->add_audiences("audiences1");
  origin->set_presenter("presenter");
  origin->add_groups("group1");
  origin->add_groups("group2");
  auto claim = origin->mutable_claims();
  (*claim)["key1"] = "value1";
  (*claim)["key2"] = "value2";
  origin->set_raw_claims("rawclaim");

  Authentication::SaveAuthAttributesToStruct(result, data);
  EXPECT_FALSE(data.mutable_fields()->empty());

  EXPECT_EQ(data.fields()
                .at(istio::utils::AttributeName::kRequestAuthPrincipal)
                .string_value(),
            "principal");
  EXPECT_EQ(
      data.fields().at(istio::utils::AttributeName::kSourceUser).string_value(),
      "peeruser");
  EXPECT_EQ(data.fields()
                .at(istio::utils::AttributeName::kSourcePrincipal)
                .string_value(),
            "peeruser");
  EXPECT_EQ(data.fields()
                .at(istio::utils::AttributeName::kRequestAuthAudiences)
                .string_value(),
            "audiences0");
  EXPECT_EQ(data.fields()
                .at(istio::utils::AttributeName::kRequestAuthGroups)
                .list_value()
                .values(0)
                .string_value(),
            "group1");
  EXPECT_EQ(data.fields()
                .at(istio::utils::AttributeName::kRequestAuthGroups)
                .list_value()
                .values(1)
                .string_value(),
            "group2");
  EXPECT_EQ(data.fields()
                .at(istio::utils::AttributeName::kRequestAuthPresenter)
                .string_value(),
            "presenter");

  auto actual_claim =
      data.fields().at(istio::utils::AttributeName::kRequestAuthClaims);
  EXPECT_EQ(actual_claim.struct_value().fields().at("key1").string_value(),
            "value1");
  EXPECT_EQ(actual_claim.struct_value().fields().at("key2").string_value(),
            "value2");

  EXPECT_EQ(data.fields()
                .at(istio::utils::AttributeName::kRequestAuthRawClaims)
                .string_value(),
            "rawclaim");
}

TEST_F(AuthenticationTest, ResultAlreadyExist) {
  request_headers_.addCopy(GetHeaderLocation(), "somedata");
  EXPECT_TRUE(Authentication::HasResultInHeader(request_headers_));
  EXPECT_FALSE(Authentication::SaveResultToHeader(Result{}, &request_headers_));
  EXPECT_TRUE(Authentication::HasResultInHeader(request_headers_));
  const auto entry = request_headers_.get(GetHeaderLocation());
  EXPECT_TRUE(entry != nullptr);
  EXPECT_EQ("somedata", entry->value().getStringView());
}

TEST_F(AuthenticationTest, FetchResultNotExit) {
  Result result;
  EXPECT_FALSE(
      Authentication::FetchResultFromHeader(request_headers_, &result));
}

TEST_F(AuthenticationTest, FetchResultBadFormat) {
  request_headers_.addCopy(GetHeaderLocation(), "somedata");
  EXPECT_TRUE(Authentication::HasResultInHeader(request_headers_));
  Result result;
  EXPECT_FALSE(
      Authentication::FetchResultFromHeader(request_headers_, &result));
}

TEST_F(AuthenticationTest, FetchResult) {
  EXPECT_TRUE(
      Authentication::SaveResultToHeader(test_result_, &request_headers_));
  Result fetch_result;
  EXPECT_TRUE(
      Authentication::FetchResultFromHeader(request_headers_, &fetch_result));
  EXPECT_TRUE(TestUtility::protoEqual(test_result_, fetch_result));
}

}  // namespace Utils
}  // namespace Envoy
