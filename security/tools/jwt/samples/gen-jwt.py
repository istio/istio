#!/usr/bin/python

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Python script generates a JWT signed with custom private key.

Example:
./gen-jwt.py  --iss example-issuer --aud foo,bar --claims=email:foo@google.com,dead:beef key.pem -listclaim key1 val2 val3 -listclaim key2 val3 val4
"""
from __future__ import print_function
import argparse
import copy
import time

from jwcrypto import jwt, jwk


def main(args):
    """Generates a signed JSON Web Token from local private key."""
    with open(args.key) as f:
        pem_data = f.read()
    f.closed

    pem_data_encode = pem_data.encode("utf-8")
    key = jwk.JWK.from_pem(pem_data_encode)

    if args.jwks:
        with open(args.jwks, "w+") as fout:
            fout.write("{ \"keys\":[ ")
            fout.write(key.export(private_key=False))
            fout.write("]}")
        fout.close

    now = int(time.time())
    payload = {
        # expire in one hour.
        "exp": now + args.expire,
        "iat": now,
    }
    if args.iss:
        payload["iss"] = args.iss
    if args.sub:
        payload["sub"] = args.sub
    else:
        payload["sub"] = args.iss

    if args.aud:
        if "," in args.aud:
            payload["aud"] = args.aud.split(",")
        else:
            payload["aud"] = args.aud

    if args.claims:
        for item in args.claims.split(","):
            k, v = item.split(':')
            payload[k] = v

    if args.listclaim:
        for item in args.listclaim:
            if (len(item) > 1):
                k = item[0]
                v = item[1:]
                payload[k] = v

    if args.nestedclaim:
        nested = {}
        for item in args.nestedclaim:
            if (len(item) > 1):
                k = item[0]
                v = item[1:]
                if len(v) == 1:
                    v = v[0]
                nested[k] = v
        nested["nested-2"] = copy.copy(nested)
        payload["nested"] = nested

    token = jwt.JWT(header={"alg": "RS256", "typ": "JWT", "kid": key.key_id},
                    claims=payload)

    token.make_signed_token(key)

    return token.serialize()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        description=__doc__,
        formatter_class=argparse.RawDescriptionHelpFormatter)
    # positional arguments
    parser.add_argument(
        'key',
        help='The path to the key pem file. The key can be generated with openssl command: `openssl genrsa -out key.pem 2048`')
    # optional arguments
    parser.add_argument("-iss", "--iss",
                        default="testing@secure.istio.io",
                        help="iss claim. Default is `testing@secure.istio.io`")
    parser.add_argument("-aud", "--aud",
                        help="aud claim. This is comma-separated-list of audiences")
    parser.add_argument("-sub", "--sub",
                        help="sub claim. If not provided, it is set to the same as iss claim.")
    parser.add_argument("-claims", "--claims",
                        help="Other claims in format name1:value1,name2:value2 etc. Only string values are supported.")
    parser.add_argument("-jwks", "--jwks",
                        help="Path to the output file for JWKS.")
    parser.add_argument("-expire", "--expire", type=int, default=3600,
                        help="JWT expiration time in second. Default is 1 hour.")
    parser.add_argument(
        "-listclaim",
        "--listclaim",
        action='append',
        nargs='+',
        help="A list claim in format key1 value2 value3... Only string values are supported. Multiple list claims can be specified, e.g., -listclaim key1 val2 val3 -listclaim key2 val3 val4.")
    parser.add_argument(
        "-nestedclaim",
        "--nestedclaim",
        action='append',
        nargs='+',
        help="Nested claim in format key value1 [value2 ...], only string values are supported, will be added under the nested key in the JWT payload. "
             "Multiple nested claims can be specified, e.g., -nestedclaim key1 val2 val3 -nestedclaim key2 val3 val4."
    )
    print(main(parser.parse_args()))
