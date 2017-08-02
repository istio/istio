/* Copyright 2017 Istio Authors. All Rights Reserved.
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

#include "jwt.h"

#include "common/common/utility.h"
#include "test/test_common/utility.h"

namespace Envoy {
namespace Http {
namespace Auth {

class JwtTest : public testing::Test {
 public:
  const std::string kJwt =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0.FxT92eaBr9thDpeWaQh0YFhblVggn86DBpnTa_"
      "DVO4mNoGEkdpuhYq3epHPAs9EluuxdSkDJ3fCoI758ggGDw8GbqyJAcOsH10fBOrQbB7EFRB"
      "CI1xz6-6GEUac5PxyDnwy3liwC_"
      "gK6p4yqOD13EuEY5aoYkeM382tDFiz5Jkh8kKbqKT7h0bhIimniXLDz6iABeNBFouczdPf04"
      "N09hdvlCtAF87Fu1qqfwEQ93A-J7m08bZJoyIPcNmTcYGHwfMR4-lcI5cC_93C_"
      "5BGE1FHPLOHpNghLuM6-rhOtgwZc9ywupn_bBK3QzuAoDnYwpqQhgQL_CdUD_bSHcmWFkw";

  const std::string kJwtHeaderEncoded = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9";
  const std::string kJwtPayloadEncoded =
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0";
  const std::string kJwtSignatureEncoded =
      "FxT92eaBr9thDpeWaQh0YFhblVggn86DBpnTa_"
      "DVO4mNoGEkdpuhYq3epHPAs9EluuxdSkDJ3fCoI758ggGDw8GbqyJAcOsH10fBOrQbB7EFRB"
      "CI1xz6-6GEUac5PxyDnwy3liwC_"
      "gK6p4yqOD13EuEY5aoYkeM382tDFiz5Jkh8kKbqKT7h0bhIimniXLDz6iABeNBFouczdPf04"
      "N09hdvlCtAF87Fu1qqfwEQ93A-J7m08bZJoyIPcNmTcYGHwfMR4-lcI5cC_93C_"
      "5BGE1FHPLOHpNghLuM6-rhOtgwZc9ywupn_bBK3QzuAoDnYwpqQhgQL_CdUD_bSHcmWFkw";

  const std::string kHeader = R"EOF({"alg":"RS256","typ":"JWT"})EOF";
  const std::string kPayload =
      R"EOF({"iss":"https://example.com","sub":"test@example.com","exp":1501281058})EOF";

  const std::string kPublicKey =
      "MIIBCgKCAQEAtw7MNxUTxmzWROCD5BqJxmzT7xqc9KsnAjbXCoqEEHDx4WBlfcwk"
      "XHt9e/2+Uwi3Arz3FOMNKwGGlbr7clBY3utsjUs8BTF0kO/poAmSTdSuGeh2mSbc"
      "VHvmQ7X/kichWwx5Qj0Xj4REU3Gixu1gQIr3GATPAIULo5lj/ebOGAa+l0wIG80N"
      "zz1pBtTIUx68xs5ZGe7cIJ7E8n4pMX10eeuh36h+aossePeuHulYmjr4N0/1jG7a"
      "+hHYL6nqwOR3ej0VqCTLS0OloC0LuCpLV7CnSpwbp2Qg/c+MDzQ0TH8g8drIzR5h"
      "Fe9a3NlNRMXgUU5RqbLnR9zfXr7b9oEszQIDAQAB";

  const std::string kPrivateKey =
      "-----BEGIN RSA PRIVATE KEY-----\n"
      "MIIEowIBAAKCAQEAtw7MNxUTxmzWROCD5BqJxmzT7xqc9KsnAjbXCoqEEHDx4WBl\n"
      "fcwkXHt9e/2+Uwi3Arz3FOMNKwGGlbr7clBY3utsjUs8BTF0kO/poAmSTdSuGeh2\n"
      "mSbcVHvmQ7X/kichWwx5Qj0Xj4REU3Gixu1gQIr3GATPAIULo5lj/ebOGAa+l0wI\n"
      "G80Nzz1pBtTIUx68xs5ZGe7cIJ7E8n4pMX10eeuh36h+aossePeuHulYmjr4N0/1\n"
      "jG7a+hHYL6nqwOR3ej0VqCTLS0OloC0LuCpLV7CnSpwbp2Qg/c+MDzQ0TH8g8drI\n"
      "zR5hFe9a3NlNRMXgUU5RqbLnR9zfXr7b9oEszQIDAQABAoIBAQCgQQ8cRZJrSkqG\n"
      "P7qWzXjBwfIDR1wSgWcD9DhrXPniXs4RzM7swvMuF1myW1/r1xxIBF+V5HNZq9tD\n"
      "Z07LM3WpqZX9V9iyfyoZ3D29QcPX6RGFUtHIn5GRUGoz6rdTHnh/+bqJ92uR02vx\n"
      "VPD4j0SNHFrWpxcE0HRxA07bLtxLgNbzXRNmzAB1eKMcrTu/W9Q1zI1opbsQbHbA\n"
      "CjbPEdt8INi9ij7d+XRO6xsnM20KgeuKx1lFebYN9TKGEEx8BCGINOEyWx1lLhsm\n"
      "V6S0XGVwWYdo2ulMWO9M0lNYPzX3AnluDVb3e1Yq2aZ1r7t/GrnGDILA1N2KrAEb\n"
      "AAKHmYNNAoGBAPAv9qJqf4CP3tVDdto9273DA4Mp4Kjd6lio5CaF8jd/4552T3UK\n"
      "N0Q7N6xaWbRYi6xsCZymC4/6DhmLG/vzZOOhHkTsvLshP81IYpWwjm4rF6BfCSl7\n"
      "ip+1z8qonrElxes68+vc1mNhor6GGsxyGe0C18+KzpQ0fEB5J4p0OHGnAoGBAMMb\n"
      "/fpr6FxXcjUgZzRlxHx1HriN6r8Jkzc+wAcQXWyPUOD8OFLcRuvikQ16sa+SlN4E\n"
      "HfhbFn17ABsikUAIVh0pPkHqMsrGFxDn9JrORXUpNhLdBHa6ZH+we8yUe4G0X4Mc\n"
      "R7c8OT26p2zMg5uqz7bQ1nJ/YWlP4nLqIytehnRrAoGAT6Rn0JUlsBiEmAylxVoL\n"
      "mhGnAYAKWZQ0F6/w7wEtPs/uRuYOFM4NY1eLb2AKLK3LqqGsUkAQx23v7PJelh2v\n"
      "z3bmVY52SkqNIGGnJuGDaO5rCCdbH2EypyCfRSDCdhUDWquSpBv3Dr8aOri2/CG9\n"
      "jQSLUOtC8ouww6Qow1UkPjMCgYB8kTicU5ysqCAAj0mVCIxkMZqFlgYUJhbZpLSR\n"
      "Tf93uiCXJDEJph2ZqLOXeYhMYjetb896qx02y/sLWAyIZ0ojoBthlhcLo2FCp/Vh\n"
      "iOSLot4lOPsKmoJji9fei8Y2z2RTnxCiik65fJw8OG6mSm4HeFoSDAWzaQ9Y8ue1\n"
      "XspVNQKBgAiHh4QfiFbgyFOlKdfcq7Scq98MA3mlmFeTx4Epe0A9xxhjbLrn362+\n"
      "ZSCUhkdYkVkly4QVYHJ6Idzk47uUfEC6WlLEAnjKf9LD8vMmZ14yWR2CingYTIY1\n"
      "LL2jMkSYEJx102t2088meCuJzEsF3BzEWOP8RfbFlciT7FFVeiM4\n"
      "-----END RSA PRIVATE KEY-----";

  /*
   * jwt with header replaced by
   * "{"alg":"RS256","typ":"JWT", this is a invalid json}"
   */
  const std::string kJwtWithBadJsonHeader =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsIHRoaXMgaXMgYSBpbnZhbGlkIGpzb259."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0."
      "ERgdOJdVCrUAaAIMaAG6rgAR7M6ZJUjvKxIMgb9jrfsEVzsetb4UlPsrO-FBA4LUT_"
      "xIshL4Bzd0_3w63v7xol2-iAQgW_7PQeeEEqqMcyfkuXEhHu_lXawAlpqKhCmFuyIeYBiSs-"
      "RRIqHCutIJSBfcIGLMRcVzpMODfwMMlzjw6SlfMuy68h54TpBt1glvwEg71lVVO7IE3Fxwgl"
      "EDR_2MrVwjmyes9TmDgsj_zBHHn_d09kVqV_adYXtVec9fyo7meODQXB_"
      "eWm065WsSRFksQn8fidWtrAfdcSzYe2wN0doP-QYzJeWKll15XVRKS67NeENz40Wd_Tr_"
      "tyHBHw";

  /*
   * jwt with payload replaced by
   * "this is not a json"
   */
  const std::string kJwtWithBadJsonPayload =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.dGhpcyBpcyBub3QgYSBqc29u."
      "NhQjffMuBkYA9MXq3Fi3h2RRR6vNsYHOjF22GRHRcAEsTHJGYpWsU0MpkWnSJ04Ktx6PFp8f"
      "jRUI0bLtLC2R2Nv3VQNfvcZy0cJmlEWGZbRjEA2AwOaMpiKX-6z5BtMic9hG5Aw1IDxf_"
      "ZvqiE5nRxPBnMXxsINgJ1veTd0zBhOsr0Y3Onl2O3UJSqrQn4kSqjpTENODjSJcJcfiy15sU"
      "MX7wCiP_FSjLAW-"
      "mcaa8RdV49LegwB185eK9UmTJ98yTqEN7w9wcKkZFe8vpojkJX8an0EjGOTJ_5IsU1A_"
      "Xv1Z1ZQYGTOEsMH8j9zWslYTRq15bDIyALHRD46UHqjDSQ";

  /*
   * jwt with header replaced by
   * "{"typ":"JWT"}"
   */
  const std::string kJwtWithAlgAbsent =
      "eyJ0eXAiOiJKV1QifQ."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0"
      "."
      "MGJmMDU2YjViZmJhMzE5MGI3MTRiMmE4NDhiMmIzNzI2Mzk3MGUwOGVmZTAwMzc0YzY4MWFj"
      "NzgzMDZjZWRlYgoyZWY3Mzk2NWE2MjYxZWI2M2FhMGFjM2E1NDQ1MjNmMjZmNjU2Y2MxYWIz"
      "YTczNGFlYTg4ZDIyN2YyZmM4YTI5CjM5OTQwNjI2ZjI3ZDlmZTM4M2JjY2NhZjIxMmJlY2U5"
      "Y2Q3NGY5YmY2YWY2ZDI2ZTEyNDllMjU4NGVhZTllMGQKMzg0YzVlZmUzY2ZkMWE1NzM4YTIw"
      "MzBmYTQ0OWY0NDQ1MTNhOTQ4NTRkMzgxMzdkMTljMWQ3ZmYxYjNlMzJkMQoxMGMyN2VjZDQ5"
      "MTMzNjZiZmE4Zjg3ZTEyNWQzMGEwYjhhYjUyYWE5YzZmZTcwM2FmZDliMjkzODY3OWYxNWQy"
      "CjZiNWIzZjgzYTk0Zjg1MjFkMDhiNmYyNzY1MTM1N2MyYWI0MzBkM2FlYjg5MTFmNjM5ZGNj"
      "MGM2NTcxNThmOWUKOWQ1ZDM2NWFkNGVjOTgwYmNkY2RiMDUzM2MzYjY2MjJmYWJiMDViNjNk"
      "NjIxMDJiZDkyZDE3ZjZkZDhiMTBkOQo1YjBlMDRiZWU2MDBjNjRhNzM0ZGE1ZGY2YjljMGY5"
      "ZDM1Mzk3MjcyNDcyN2RjMTViYjk1MjMwZjdmYmU5MzYx";

  /*
   * jwt with header replaced by
   * "{"alg":256,"typ":"JWT"}"
   */
  const std::string kJwtWithAlgIsNotString =
      "eyJhbGciOjI1NiwidHlwIjoiSldUIn0."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0."
      "ODYxMDhhZjY5MjEyMGQ4ZjE5YzMzYmQzZDY3MmE1NjFjNDM1NzdhYmNhNDM0Njg2MWMwNGI4"
      "ZDNhZDExNjUxZgphZTU0ZjMzZWVmMWMzYmQyOTEwNGIxNTA3ZDllZTI0ZmY0OWFkZTYwNGUz"
      "MGUzMWIxN2MwMTIzNTY0NDYzNjBlCjEyZDk3ZGRiMDAwZDgwMDFmYjcwOTIzZDYzY2VhMzE1"
      "MjcyNzdlY2RhYzZkMWU5MThmOThjOTFkNTZiM2NhYWIKNjA0ZThiNWI4N2MwNWM4M2RkODE4"
      "NWYwNDBiYjY4Yjk3MmY5MDc2YmYzYTk3ZjM0OWVhYjA1ZTdjYzdhOGEzZApjMGU4Y2Y0MzJl"
      "NzY2MDAwYTQ0ZDg1ZmE5MjgzM2ExNjNjOGM3OTllMTEyNTM5OWMzYzY3MThiMzY2ZjU5YTVl"
      "CjVjYTdjZTBmNDdlMjZhYjU3M2Y2NDI4ZmRmYzQ2N2NjZjQ4OWFjNTA1OTRhM2NlYTlhNTE1"
      "ODJhMDE1ODA2YzkKZmRhNTFmODliNTk3NjA4Njg2NzNiMDUwMzdiY2IzOTQzMzViYzU2YmFk"
      "ODUyOWIwZWJmMjc1OTkxMTkzZjdjMwo0MDFjYWRlZDI4NjA2MmNlZTFhOGU3YWFiMDJiNjcy"
      "NGVhYmVmMjA3MGQyYzFlMmY3NDRiM2IyNjU0MGQzZmUx";

  /*
   * jwt with header replaced by
   * "{"alg":"InvalidAlg","typ":"JWT"}"
   */
  const std::string kJwtWithInvalidAlg =
      "eyJhbGciOiJJbnZhbGlkQWxnIiwidHlwIjoiSldUIn0."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0."
      "MjQ3ZThmMTQ1YWYwZDIyZjVlZDlhZTJhOWIzYWI2OGY5ZTcyZWU1ZmJlNzA1ODE2NjkxZDU3"
      "OGY0MmU0OTlhNgpiMmY0NmM2OTI3Yjc1Mjk3NDdhYTQyZTY3Mjk2NGY0MzkzMzgwMjhlNjE2"
      "ZDk2YWM4NDQwZTQ1MGRiYTM5NjJmCjNlODU0YjljOTNjOTg4YTZmNjVkNDhiYmViNTBkZTg5"
      "NWZjOGNmM2NmY2I0MGY1MmU0YjQwMWFjYWZlMjU0M2EKMzc3MjU2YzgyNmZlODIxYTgxNDZm"
      "ZDZkODhkZjg3Yzg1MjJkYTM4MWI4MmZiNTMwOGYxODAzMGZjZGNjMjAxYgpmYmM2NzRiZGQ5"
      "YWMyNzYwZDliYzBlMTMwMDA2OTE3MTBmM2U5YmZlN2Y4OGYwM2JjMWFhNTAwZTY2ZmVhMDk5"
      "CjlhYjVlOTFiZGVkNGMxZTBmMzBiNTdiOGM0MDY0MGViNjMyNTE2Zjc5YTczNzM0YTViM2M2"
      "YjAxMGQ4MjYyYmUKM2U1MjMyMTE4MzUxY2U5M2VkNmY1NWJhYTFmNmU5M2NmMzVlZjJiNjRi"
      "MDYxNzU4YWJmYzdkNzUzYzAxMWVhNgo3NTg1N2MwMGY3YTE3Y2E3YWI2NGJlMWIyYjdkNzZl"
      "NWJlMThhZWFmZWY5NDU5MjAxY2RkY2NkZGZiZjczMjQ2";
};

TEST_F(JwtTest, JwtDecode) {
  auto payload = Jwt::Decode(kJwt, kPublicKey);

  EXPECT_TRUE(payload);

  EXPECT_TRUE((*payload)["iss"].IsString());
  std::string iss = (*payload)["iss"].GetString();
  EXPECT_STREQ("https://example.com", iss.c_str());

  EXPECT_TRUE((*payload)["sub"].IsString());
  std::string sub = (*payload)["sub"].GetString();
  EXPECT_STREQ("test@example.com", sub.c_str());

  EXPECT_TRUE((*payload)["exp"].IsInt64());
  int64_t exp = (*payload)["exp"].GetInt64();
  EXPECT_EQ(1501281058LL, exp);
}

TEST_F(JwtTest, InvalidSignature) {
  auto invalid_jwt = kJwt;
  invalid_jwt[kJwt.length() - 1] = kJwt[kJwt.length() - 1] != 'a' ? 'a' : 'b';

  auto payload = Jwt::Decode(invalid_jwt, kPublicKey);

  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, InvalidPublickey) {
  auto invalid_pubkey = kPublicKey;
  invalid_pubkey[0] = kPublicKey[0] != 'a' ? 'a' : 'b';

  auto payload = Jwt::Decode(kJwt, invalid_pubkey);

  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, Base64urlBadInputHeader) {
  auto invalid_header = kJwtHeaderEncoded + 'a';
  auto invalid_jwt = StringUtil::join(
      std::vector<std::string>{invalid_header, kJwtPayloadEncoded,
                               kJwtSignatureEncoded},
      ".");

  auto payload = Jwt::Decode(invalid_jwt, kPublicKey);

  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, Base64urlBadInputPayload) {
  auto invalid_payload = kJwtPayloadEncoded + 'a';
  auto invalid_jwt = StringUtil::join(
      std::vector<std::string>{kJwtHeaderEncoded, invalid_payload,
                               kJwtSignatureEncoded},
      ".");

  auto payload = Jwt::Decode(invalid_jwt, kPublicKey);

  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, Base64urlBadinputSignature) {
  auto invalid_signature = kJwtSignatureEncoded + 'a';
  auto invalid_jwt = StringUtil::join(
      std::vector<std::string>{kJwtHeaderEncoded, kJwtPayloadEncoded,
                               invalid_signature},
      ".");

  auto payload = Jwt::Decode(invalid_jwt, kPublicKey);

  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, JwtInvalidNumberOfDots) {
  auto invalid_jwt = kJwt + '.';
  auto payload = Jwt::Decode(invalid_jwt, kPublicKey);
  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, JsonBadInputHeader) {
  auto payload = Jwt::Decode(kJwtWithBadJsonHeader, kPublicKey);
  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, JsonBadInputPayload) {
  auto payload = Jwt::Decode(kJwtWithBadJsonPayload, kPublicKey);
  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, AlgAbsentInHeader) {
  auto payload = Jwt::Decode(kJwtWithAlgAbsent, kPublicKey);
  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, AlgIsNotString) {
  auto payload = Jwt::Decode(kJwtWithAlgIsNotString, kPublicKey);
  EXPECT_FALSE(payload);
}

TEST_F(JwtTest, InvalidAlg) {
  auto payload = Jwt::Decode(kJwtWithInvalidAlg, kPublicKey);
  EXPECT_FALSE(payload);
}

}  // namespace Auth
}  // namespace Http
}  // namespace Envoy
