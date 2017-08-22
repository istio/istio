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
  // JWT with
  // Header:  {"alg":"RS256","typ":"JWT"}
  // Payload:
  // {"iss":"https://example.com","sub":"test@example.com","exp":1501281058}
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

  const std::string kPublicKey =
      "MIIBCgKCAQEAtw7MNxUTxmzWROCD5BqJxmzT7xqc9KsnAjbXCoqEEHDx4WBlfcwk"
      "XHt9e/2+Uwi3Arz3FOMNKwGGlbr7clBY3utsjUs8BTF0kO/poAmSTdSuGeh2mSbc"
      "VHvmQ7X/kichWwx5Qj0Xj4REU3Gixu1gQIr3GATPAIULo5lj/ebOGAa+l0wIG80N"
      "zz1pBtTIUx68xs5ZGe7cIJ7E8n4pMX10eeuh36h+aossePeuHulYmjr4N0/1jG7a"
      "+hHYL6nqwOR3ej0VqCTLS0OloC0LuCpLV7CnSpwbp2Qg/c+MDzQ0TH8g8drIzR5h"
      "Fe9a3NlNRMXgUU5RqbLnR9zfXr7b9oEszQIDAQAB";

  //  private key:
  //      "-----BEGIN RSA PRIVATE KEY-----"
  //      "MIIEowIBAAKCAQEAtw7MNxUTxmzWROCD5BqJxmzT7xqc9KsnAjbXCoqEEHDx4WBl"
  //      "fcwkXHt9e/2+Uwi3Arz3FOMNKwGGlbr7clBY3utsjUs8BTF0kO/poAmSTdSuGeh2"
  //      "mSbcVHvmQ7X/kichWwx5Qj0Xj4REU3Gixu1gQIr3GATPAIULo5lj/ebOGAa+l0wI"
  //      "G80Nzz1pBtTIUx68xs5ZGe7cIJ7E8n4pMX10eeuh36h+aossePeuHulYmjr4N0/1"
  //      "jG7a+hHYL6nqwOR3ej0VqCTLS0OloC0LuCpLV7CnSpwbp2Qg/c+MDzQ0TH8g8drI"
  //      "zR5hFe9a3NlNRMXgUU5RqbLnR9zfXr7b9oEszQIDAQABAoIBAQCgQQ8cRZJrSkqG"
  //      "P7qWzXjBwfIDR1wSgWcD9DhrXPniXs4RzM7swvMuF1myW1/r1xxIBF+V5HNZq9tD"
  //      "Z07LM3WpqZX9V9iyfyoZ3D29QcPX6RGFUtHIn5GRUGoz6rdTHnh/+bqJ92uR02vx"
  //      "VPD4j0SNHFrWpxcE0HRxA07bLtxLgNbzXRNmzAB1eKMcrTu/W9Q1zI1opbsQbHbA"
  //      "CjbPEdt8INi9ij7d+XRO6xsnM20KgeuKx1lFebYN9TKGEEx8BCGINOEyWx1lLhsm"
  //      "V6S0XGVwWYdo2ulMWO9M0lNYPzX3AnluDVb3e1Yq2aZ1r7t/GrnGDILA1N2KrAEb"
  //      "AAKHmYNNAoGBAPAv9qJqf4CP3tVDdto9273DA4Mp4Kjd6lio5CaF8jd/4552T3UK"
  //      "N0Q7N6xaWbRYi6xsCZymC4/6DhmLG/vzZOOhHkTsvLshP81IYpWwjm4rF6BfCSl7"
  //      "ip+1z8qonrElxes68+vc1mNhor6GGsxyGe0C18+KzpQ0fEB5J4p0OHGnAoGBAMMb"
  //      "/fpr6FxXcjUgZzRlxHx1HriN6r8Jkzc+wAcQXWyPUOD8OFLcRuvikQ16sa+SlN4E"
  //      "HfhbFn17ABsikUAIVh0pPkHqMsrGFxDn9JrORXUpNhLdBHa6ZH+we8yUe4G0X4Mc"
  //      "R7c8OT26p2zMg5uqz7bQ1nJ/YWlP4nLqIytehnRrAoGAT6Rn0JUlsBiEmAylxVoL"
  //      "mhGnAYAKWZQ0F6/w7wEtPs/uRuYOFM4NY1eLb2AKLK3LqqGsUkAQx23v7PJelh2v"
  //      "z3bmVY52SkqNIGGnJuGDaO5rCCdbH2EypyCfRSDCdhUDWquSpBv3Dr8aOri2/CG9"
  //      "jQSLUOtC8ouww6Qow1UkPjMCgYB8kTicU5ysqCAAj0mVCIxkMZqFlgYUJhbZpLSR"
  //      "Tf93uiCXJDEJph2ZqLOXeYhMYjetb896qx02y/sLWAyIZ0ojoBthlhcLo2FCp/Vh"
  //      "iOSLot4lOPsKmoJji9fei8Y2z2RTnxCiik65fJw8OG6mSm4HeFoSDAWzaQ9Y8ue1"
  //      "XspVNQKBgAiHh4QfiFbgyFOlKdfcq7Scq98MA3mlmFeTx4Epe0A9xxhjbLrn362+"
  //      "ZSCUhkdYkVkly4QVYHJ6Idzk47uUfEC6WlLEAnjKf9LD8vMmZ14yWR2CingYTIY1"
  //      "LL2jMkSYEJx102t2088meCuJzEsF3BzEWOP8RfbFlciT7FFVeiM4"
  //      "-----END RSA PRIVATE KEY-----";

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

class JwtTestWithJwk : public testing::Test {
 public:
  // The following public key jwk and token are taken from
  // https://github.com/cloudendpoints/esp/blob/master/src/api_manager/auth/lib/auth_jwt_validator_test.cc
  const std::string kPublicKey =
      "{\"keys\": [{\"kty\": \"RSA\",\"alg\": \"RS256\",\"use\": "
      "\"sig\",\"kid\": \"62a93512c9ee4c7f8067b5a216dade2763d32a47\",\"n\": "
      "\"0YWnm_eplO9BFtXszMRQNL5UtZ8HJdTH2jK7vjs4XdLkPW7YBkkm_"
      "2xNgcaVpkW0VT2l4mU3KftR-6s3Oa5Rnz5BrWEUkCTVVolR7VYksfqIB2I_"
      "x5yZHdOiomMTcm3DheUUCgbJRv5OKRnNqszA4xHn3tA3Ry8VO3X7BgKZYAUh9fyZTFLlkeAh"
      "0-"
      "bLK5zvqCmKW5QgDIXSxUTJxPjZCgfx1vmAfGqaJb-"
      "nvmrORXQ6L284c73DUL7mnt6wj3H6tVqPKA27j56N0TB1Hfx4ja6Slr8S4EB3F1luYhATa1P"
      "KU"
      "SH8mYDW11HolzZmTQpRoLV8ZoHbHEaTfqX_aYahIw\",\"e\": \"AQAB\"},{\"kty\": "
      "\"RSA\",\"alg\": \"RS256\",\"use\": \"sig\",\"kid\": "
      "\"b3319a147514df7ee5e4bcdee51350cc890cc89e\",\"n\": "
      "\"qDi7Tx4DhNvPQsl1ofxxc2ePQFcs-L0mXYo6TGS64CY_"
      "2WmOtvYlcLNZjhuddZVV2X88m0MfwaSA16wE-"
      "RiKM9hqo5EY8BPXj57CMiYAyiHuQPp1yayjMgoE1P2jvp4eqF-"
      "BTillGJt5W5RuXti9uqfMtCQdagB8EC3MNRuU_KdeLgBy3lS3oo4LOYd-"
      "74kRBVZbk2wnmmb7IhP9OoLc1-7-9qU1uhpDxmE6JwBau0mDSwMnYDS4G_ML17dC-"
      "ZDtLd1i24STUw39KH0pcSdfFbL2NtEZdNeam1DDdk0iUtJSPZliUHJBI_pj8M-2Mn_"
      "oA8jBuI8YKwBqYkZCN1I95Q\",\"e\": \"AQAB\"}]}";

  //  private key:
  //      "-----BEGIN PRIVATE KEY-----\n"
  //      "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCoOLtPHgOE289C\n"
  //      "yXWh/HFzZ49AVyz4vSZdijpMZLrgJj/ZaY629iVws1mOG511lVXZfzybQx/BpIDX\n"
  //      "rAT5GIoz2GqjkRjwE9ePnsIyJgDKIe5A+nXJrKMyCgTU/aO+nh6oX4FOKWUYm3lb\n"
  //      "lG5e2L26p8y0JB1qAHwQLcw1G5T8p14uAHLeVLeijgs5h37viREFVluTbCeaZvsi\n"
  //      "E/06gtzX7v72pTW6GkPGYTonAFq7SYNLAydgNLgb8wvXt0L5kO0t3WLbhJNTDf0o\n"
  //      "fSlxJ18VsvY20Rl015qbUMN2TSJS0lI9mWJQckEj+mPwz7Yyf+gDyMG4jxgrAGpi\n"
  //      "RkI3Uj3lAgMBAAECggEAOuaaVyp4KvXYDVeC07QTeUgCdZHQkkuQemIi5YrDkCZ0\n"
  //      "Zsi6CsAG/f4eVk6/BGPEioItk2OeY+wYnOuDVkDMazjUpe7xH2ajLIt3DZ4W2q+k\n"
  //      "v6WyxmmnPqcZaAZjZiPxMh02pkqCNmqBxJolRxp23DtSxqR6lBoVVojinpnIwem6\n"
  //      "xyUl65u0mvlluMLCbKeGW/K9bGxT+qd3qWtYFLo5C3qQscXH4L0m96AjGgHUYW6M\n"
  //      "Ffs94ETNfHjqICbyvXOklabSVYenXVRL24TOKIHWkywhi1wW+Q6zHDADSdDVYw5l\n"
  //      "DaXz7nMzJ2X7cuRP9zrPpxByCYUZeJDqej0Pi7h7ZQKBgQDdI7Yb3xFXpbuPd1VS\n"
  //      "tNMltMKzEp5uQ7FXyDNI6C8+9TrjNMduTQ3REGqEcfdWA79FTJq95IM7RjXX9Aae\n"
  //      "p6cLekyH8MDH/SI744vCedkD2bjpA6MNQrzNkaubzGJgzNiZhjIAqnDAD3ljHI61\n"
  //      "NbADc32SQMejb6zlEh8hssSsXwKBgQDCvXhTIO/EuE/y5Kyb/4RGMtVaQ2cpPCoB\n"
  //      "GPASbEAHcsRk+4E7RtaoDQC1cBRy+zmiHUA9iI9XZyqD2xwwM89fzqMj5Yhgukvo\n"
  //      "XMxvMh8NrTneK9q3/M3mV1AVg71FJQ2oBr8KOXSEbnF25V6/ara2+EpH2C2GDMAo\n"
  //      "pgEnZ0/8OwKBgFB58IoQEdWdwLYjLW/d0oGEWN6mRfXGuMFDYDaGGLuGrxmEWZdw\n"
  //      "fzi4CquMdgBdeLwVdrLoeEGX+XxPmCEgzg/FQBiwqtec7VpyIqhxg2J9V2elJS9s\n"
  //      "PB1rh9I4/QxRP/oO9h9753BdsUU6XUzg7t8ypl4VKRH3UCpFAANZdW1tAoGAK4ad\n"
  //      "tjbOYHGxrOBflB5wOiByf1JBZH4GBWjFf9iiFwgXzVpJcC5NHBKL7gG3EFwGba2M\n"
  //      "BjTXlPmCDyaSDlQGLavJ2uQar0P0Y2MabmANgMkO/hFfOXBPtQQe6jAfxayaeMvJ\n"
  //      "N0fQOylUQvbRTodTf2HPeG9g/W0sJem0qFH3FrECgYEAnwixjpd1Zm/diJuP0+Lb\n"
  //      "YUzDP+Afy78IP3mXlbaQ/RVd7fJzMx6HOc8s4rQo1m0Y84Ztot0vwm9+S54mxVSo\n"
  //      "6tvh9q0D7VLDgf+2NpnrDW7eMB3n0SrLJ83Mjc5rZ+wv7m033EPaWSr/TFtc/MaF\n"
  //      "aOI20MEe3be96HHuWD3lTK0=\n"
  //      "-----END PRIVATE KEY-----";

  // JWT without kid
  // Header:  {"alg":"RS256","typ":"JWT"}
  // Payload:
  // {"iss":"https://example.com","sub":"test@example.com","exp":1501281058}
  const std::string kJwtNoKid =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0.XYPg6VPrq-H1Kl-kgmAfGFomVpnmdZLIAo0g6dhJb2Be_"
      "koZ2T76xg5_Lr828hsLKxUfzwNxl5-k1cdz_kAst6vei0hdnOYqRQ8EhkZS_"
      "5Y2vWMrzGHw7AUPKCQvSnNqJG5HV8YdeOfpsLhQTd-"
      "tG61q39FWzJ5Ra5lkxWhcrVDQFtVy7KQrbm2dxhNEHAR2v6xXP21p1T5xFBdmGZbHFiH63N9"
      "dwdRgWjkvPVTUqxrZil7PSM2zg_GTBETp_"
      "qS7Wwf8C0V9o2KZu0KDV0j0c9nZPWTv3IMlaGZAtQgJUeyemzRDtf4g2yG3xBZrLm3AzDUj_"
      "EX_pmQAHA5ZjPVCAw";

  // JWT with correct kid
  // Header:
  // {"alg":"RS256","typ":"JWT","kid":"b3319a147514df7ee5e4bcdee51350cc890cc89e"}
  // Payload:
  // {"iss":"https://example.com","sub":"test@example.com","exp":1501281058}
  const std::string kJwtWithCorrectKid =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6ImIzMzE5YTE0NzUxNGRmN2VlNWU0"
      "YmNkZWU1MTM1MGNjODkwY2M4OWUifQ."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0.QYWtQR2JNhLBJXtpJfFisF0WSyzLbD-9dynqwZt_"
      "KlQZAIoZpr65BRNEyRzpt0jYrk7RA7hUR2cS9kB3AIKuWA8kVZubrVhSv_fiX6phjf_"
      "bZYj92kDtMiPJf7RCuGyMgKXwwf4b1Sr67zamcTmQXf26DT415rnrUHVqTlOIW50TjNa1bbO"
      "fNyKZC3LFnKGEzkfaIeXYdGiSERVOTtOFF5cUtZA2OVyeAT3mE1NuBWxz0v7xJ4zdIwHwxFU"
      "wd_5tB57j_"
      "zCEC9NwnwTiZ8wcaSyMWc4GJUn4bJs22BTNlRt5ElWl6RuBohxZA7nXwWig5CoLZmCpYpb8L"
      "fBxyCpqJQ";

  // JWT with existing but incorrect kid
  // Header:
  // {"alg":"RS256","typ":"JWT","kid":"62a93512c9ee4c7f8067b5a216dade2763d32a47"}
  // Payload:
  // {"iss":"https://example.com","sub":"test@example.com","exp":1501281058}
  const std::string kJwtWithIncorrectKid =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6IjYyYTkzNTEyYzllZTRjN2Y4MDY3"
      "YjVhMjE2ZGFkZTI3NjNkMzJhNDcifQ."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0."
      "adrKqsjKh4zdOuw9rMZr0Kn2LLYG1OUfDuvnO6tk75NKCHpKX6oI8moNYhgcCQU4AoCKXZ_"
      "u-oMl54QTx9lX9xZ2VUWKTxcJEOnpoJb-DVv_FgIG9ETe5wcCS8Y9pQ2-hxtO1_LWYok1-"
      "A01Q4929u6WNw_Og4rFXR6VSpZxXHOQrEwW44D2-Lngu1PtPjWIz3rO6cOiYaTGCS6-"
      "TVeLFnB32KQg823WhFhWzzHjhYRO7NOrl-IjfGn3zYD_"
      "DfSoMY3A6LeOFCPp0JX1gcKcs2mxaF6e3LfVoBiOBZGvgG_"
      "jx3y85hF2BZiANbSf1nlLQFdjk_CWbLPhTWeSfLXMOg";

  // JWT with nonexist kid
  // Header:  {"alg":"RS256","typ":"JWT","kid":"blahblahblah"}
  // Payload:
  // {"iss":"https://example.com","sub":"test@example.com","exp":1501281058}
  const std::string kJwtWithNonExistKid =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6ImJsYWhibGFoYmxhaCJ9."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0.digk0Fr_IdcWgJNVyeVDw2dC1cQG6LsHwg5pIN93L4_"
      "xhEDI3ZFoZ8aE44kvQHWLicnHDlhELqtF-"
      "TqxrhfnitpLE7jiyknSu6NVXxtRBcZ3dOTKryVJDvDXcYXOaaP8infnh82loHfhikgg1xmk9"
      "rcH50jtc3BkxWNbpNgPyaAAE2tEisIInaxeX0gqkwiNVrLGe1hfwdtdlWFL1WENGlyniQBvB"
      "Mwi8DgG_F0eyFKTSRWoaNQQXQruEK0YIcwDj9tkYOXq8cLAnRK9zSYc5-"
      "15Hlzfb8eE77pID0HZN-Axeui4IY22I_kYftd0OEqlwXJv_v5p6kNaHsQ9QbtAkw";

  // JWT with bad-formatted kid
  // Header:  {"alg":"RS256","typ":"JWT","kid":1}
  // Payload:
  // {"iss":"https://example.com","sub":"test@example.com","exp":1501281058}
  const std::string kJwtWithBadFormatKid =
      "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6MX0."
      "eyJpc3MiOiJodHRwczovL2V4YW1wbGUuY29tIiwic3ViIjoidGVzdEBleGFtcGxlLmNvbSIs"
      "ImV4cCI6MTUwMTI4MTA1OH0."
      "oYq0UkokShprH2YO5b84CI5fEu0sKWmEJimyJQ9YZbvaGtf6zaLbdVJBTbh6plBno-"
      "miUhjqXZtDdmBexQzp5HPHoIUwQxlGggCuJRdEnmw65Ul9WFWtS7M9g8DqVKaCo9MO-"
      "apCsylPZsRSzzZuaTPorZktELt6XcUIxeXOKOSZJ78sHsRrDeLhlELd9Q0b6hzAdDEYCvYE6"
      "woc3DiRHk19nsEgdg5O1RWKjTAcdd3oD9ecznzvVmAZT8gXrGXPd49tn1qHkVr1G621Ypi9V"
      "37BD2KXH3jN9_EBocxwcxhkPwSLtP3dgkfls_f5GoWCgmp-c5ycIskCDcIjxRnPjg";
};

TEST_F(JwtTest, JwtDecode) {
  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromPem(kPublicKey), kJwt);

  EXPECT_TRUE(payload);
  EXPECT_EQ(v.GetStatus(), Status::OK);

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
  invalid_jwt[kJwt.length() - 2] = kJwt[kJwt.length() - 2] != 'a' ? 'a' : 'b';

  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromPem(kPublicKey), invalid_jwt);

  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_INVALID_SIGNATURE);
}

TEST_F(JwtTest, InvalidPublickey) {
  auto invalid_pubkey = kPublicKey;
  invalid_pubkey[0] = kPublicKey[0] != 'a' ? 'a' : 'b';

  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromPem(invalid_pubkey), kJwt);

  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::PEM_PUBKEY_PARSE_ERROR);
}

TEST_F(JwtTest, PublickeyInvalidBase64) {
  auto invalid_pubkey = "a";

  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromPem(invalid_pubkey), kJwt);

  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::PEM_PUBKEY_BAD_BASE64);
}

TEST_F(JwtTest, Base64urlBadInputHeader) {
  auto invalid_header = kJwtHeaderEncoded + "a";
  auto invalid_jwt = StringUtil::join(
      std::vector<std::string>{invalid_header, kJwtPayloadEncoded,
                               kJwtSignatureEncoded},
      ".");

  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromPem(kPublicKey), invalid_jwt);

  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_HEADER_PARSE_ERROR);
}

TEST_F(JwtTest, Base64urlBadInputPayload) {
  auto invalid_payload = kJwtPayloadEncoded + "a";
  auto invalid_jwt = StringUtil::join(
      std::vector<std::string>{kJwtHeaderEncoded, invalid_payload,
                               kJwtSignatureEncoded},
      ".");

  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromPem(kPublicKey), invalid_jwt);

  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_INVALID_SIGNATURE);
}

TEST_F(JwtTest, Base64urlBadinputSignature) {
  auto invalid_signature = "a";
  auto invalid_jwt = StringUtil::join(
      std::vector<std::string>{kJwtHeaderEncoded, kJwtPayloadEncoded,
                               invalid_signature},
      ".");

  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromPem(kPublicKey), invalid_jwt);

  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_SIGNATURE_PARSE_ERROR);
}

TEST_F(JwtTest, JwtInvalidNumberOfDots) {
  auto invalid_jwt = kJwt + '.';
  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromPem(kPublicKey), invalid_jwt);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_BAD_FORMAT);
}

TEST_F(JwtTest, JsonBadInputHeader) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromPem(kPublicKey), kJwtWithBadJsonHeader);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_HEADER_PARSE_ERROR);
}

TEST_F(JwtTest, JsonBadInputPayload) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromPem(kPublicKey), kJwtWithBadJsonPayload);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_PAYLOAD_PARSE_ERROR);
}

TEST_F(JwtTest, AlgAbsentInHeader) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromPem(kPublicKey), kJwtWithAlgAbsent);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_HEADER_NO_ALG);
}

TEST_F(JwtTest, AlgIsNotString) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromPem(kPublicKey), kJwtWithAlgIsNotString);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_HEADER_BAD_ALG);
}

TEST_F(JwtTest, InvalidAlg) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromPem(kPublicKey), kJwtWithInvalidAlg);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::ALG_NOT_IMPLEMENTED);
}

TEST_F(JwtTestWithJwk, JwtDecodeWithJwk) {
  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromJwks(kPublicKey), kJwtNoKid);
  EXPECT_TRUE(payload);
  EXPECT_EQ(v.GetStatus(), Status::OK);

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

TEST_F(JwtTestWithJwk, CorrectKid) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromJwks(kPublicKey), kJwtWithCorrectKid);

  EXPECT_EQ(v.GetStatus(), Status::OK);

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

TEST_F(JwtTestWithJwk, IncorrectKid) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromJwks(kPublicKey), kJwtWithIncorrectKid);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_INVALID_SIGNATURE);
}

TEST_F(JwtTestWithJwk, NonExistKid) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromJwks(kPublicKey), kJwtWithNonExistKid);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::KID_ALG_UNMATCH);
}

TEST_F(JwtTestWithJwk, BadFormatKid) {
  JwtVerifier v;
  auto payload =
      v.Decode(*Pubkeys::ParseFromJwks(kPublicKey), kJwtWithBadFormatKid);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWT_HEADER_BAD_KID);
}

TEST_F(JwtTestWithJwk, JwkBadJson) {
  std::string invalid_pubkey = "foobar";
  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromJwks(invalid_pubkey), kJwtNoKid);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWK_PARSE_ERROR);
}

TEST_F(JwtTestWithJwk, JwkNoKeys) {
  std::string invalid_pubkey = R"EOF({"foo":"bar"})EOF";
  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromJwks(invalid_pubkey), kJwtNoKid);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWK_NO_KEYS);
}

TEST_F(JwtTestWithJwk, JwkBadKeys) {
  std::string invalid_pubkey = R"EOF({"keys":"foobar"})EOF";
  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromJwks(invalid_pubkey), kJwtNoKid);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWK_BAD_KEYS);
}

TEST_F(JwtTestWithJwk, JwkBadPublicKey) {
  std::string invalid_pubkey = R"EOF({"keys":[]})EOF";
  JwtVerifier v;
  auto payload = v.Decode(*Pubkeys::ParseFromJwks(invalid_pubkey), kJwtNoKid);
  EXPECT_FALSE(payload);
  EXPECT_EQ(v.GetStatus(), Status::JWK_NO_VALID_PUBKEY);
}

}  // namespace Auth
}  // namespace Http
}  // namespace Envoy
