#
# Copyright 2018 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
#
# Run from the top level productpage directory with:
#
# pip install -r test-requirements.txt
# python -m unittest discover tests/unit

import unittest

import requests_mock
from markupsafe import Markup

import productpage


class ApplianceTest(unittest.TestCase):

    def setUp(self):
        self.app = productpage.app.test_client()

    @requests_mock.Mocker()
    def test_header_propagation_reviews(self, m):
        """ Check that tracing headers are forwarded correctly """
        product_id = 0
        # Register expected headers with the mock. If the headers
        # don't match, the mock won't fire, an E500 will be triggered
        # and the test will fail.
        expected_headers = {
            'x-request-id': '34eeb41d-d267-9e49-8b84-dde403fc5b72',
            'x-b3-traceid': '80f198ee56343ba864fe8b2a57d3eff7',
            'x-b3-spanid': 'e457b5a2e4d86bd1',
            'x-b3-sampled': '1',
            'sw8': '40c7fdf104e3de67'
        }
        m.get("http://reviews:9080/reviews/%d" % product_id, text='{}',
              request_headers=expected_headers)

        uri = "/api/v1/products/%d/reviews" % product_id
        headers = {
            'x-request-id': '34eeb41d-d267-9e49-8b84-dde403fc5b72',
            'x-b3-traceid': '80f198ee56343ba864fe8b2a57d3eff7',
            'x-b3-spanid': 'e457b5a2e4d86bd1',
            'x-b3-sampled': '1',
            'sw8': '40c7fdf104e3de67'
        }
        actual = self.app.get(uri, headers=headers)
        self.assertEqual(200, actual.status_code)

    @requests_mock.Mocker()
    def test_header_propagation_ratings(self, m):
        """ Check that tracing headers are forwarded correctly """
        product_id = 0
        # Register expected headers with the mock. If the headers
        # don't match, the mock won't fire, an E500 will be triggered
        # and the test will fail.
        expected_headers = {
            'x-request-id': '34eeb41d-d267-9e49-8b84-dde403fc5b73',
            'x-b3-traceid': '80f198ee56343ba864fe8b2a57d3eff7',
            'x-b3-spanid': 'e457b5a2e4d86bd1',
            'x-b3-sampled': '1',
            'sw8': '40c7fdf104e3de67'
        }
        m.get("http://ratings:9080/ratings/%d" % product_id, text='{}',
              request_headers=expected_headers)

        uri = "/api/v1/products/%d/ratings" % product_id
        headers = {
            'x-request-id': '34eeb41d-d267-9e49-8b84-dde403fc5b73',
            'x-b3-traceid': '80f198ee56343ba864fe8b2a57d3eff7',
            'x-b3-spanid': 'e457b5a2e4d86bd1',
            'x-b3-sampled': '1',
            'sw8': '40c7fdf104e3de67'
        }
        actual = self.app.get(uri, headers=headers)
        print(actual.data)
        self.assertEqual(200, actual.status_code)

    @requests_mock.Mocker()
    def test_invalid_product_id(self, m):
        """ Check that only numeric product ids are sent to the backends """
        # No URL is registered with the mock, so any backend request would
        # fail and turn into a 500 instead of the expected 400.
        uris = [
            '/api/v1/products/..',
            '/api/v1/products/0%3Fx=1',
            '/api/v1/products/..%3F/reviews',
            '/api/v1/products/..%252Fhealth/ratings',
            # Non-ASCII digits: Arabic-Indic zero and fullwidth one
            '/api/v1/products/%D9%A0',
            '/api/v1/products/%EF%BC%91/reviews',
        ]
        for uri in uris:
            actual = self.app.get(uri)
            self.assertEqual(400, actual.status_code, uri)
        self.assertEqual(0, m.call_count)

    @requests_mock.Mocker()
    def test_login_end_user(self, m):
        """ Check that a logged in user is forwarded as the end-user header """
        product_id = 0
        m.get("http://reviews:9080/reviews/%d" % product_id, text='{}',
              request_headers={'end-user': 'jason'})

        actual = self.app.post('/login', data={'username': 'jason', 'passwd': 'password'})
        self.assertEqual(302, actual.status_code)
        actual = self.app.get("/api/v1/products/%d/reviews" % product_id)
        self.assertEqual(200, actual.status_code)

    @requests_mock.Mocker()
    def test_session_user_is_escaped(self, m):
        """ Check that the user from the session is only used as a plain string """
        product_id = 0
        m.get("http://details:9080/details/%d" % product_id, text='{}')
        m.get("http://reviews:9080/reviews/%d" % product_id, text='{}')

        with self.app.session_transaction() as sess:
            sess['user'] = Markup('<b>jason</b>')
        actual = self.app.get('/productpage')
        self.assertEqual(200, actual.status_code)
        self.assertNotIn(b'<b>jason</b>', actual.data)
        self.assertIn(b'&lt;b&gt;jason&lt;/b&gt;', actual.data)
        for req in m.request_history:
            self.assertEqual('<b>jason</b>', req.headers['end-user'])

        # Values that are not strings are ignored
        for user in [42, ['jason'], b'jason']:
            with self.app.session_transaction() as sess:
                sess['user'] = user
            calls = m.call_count
            actual = self.app.get('/productpage')
            self.assertEqual(200, actual.status_code)
            self.assertIn(b'id="sign-in-button"', actual.data, user)
            for req in m.request_history[calls:]:
                self.assertNotIn('end-user', req.headers, user)

    def test_login_logout_redirect(self):
        """ Check that login and logout only redirect to a referrer on the same host """
        data = {'username': 'jason', 'passwd': 'password'}

        actual = self.app.post('/login', data=data)
        self.assertEqual(302, actual.status_code)
        self.assertEqual('productpage', actual.headers['Location'])
        self.assertIn('session=', actual.headers['Set-Cookie'])

        for host, referrer in [('localhost', 'http://localhost/productpage?u=normal'),
                               ('localhost:9080', 'https://localhost:9080/productpage?u=test')]:
            headers = {'Host': host, 'Referer': referrer}
            actual = self.app.post('/login', data=data, headers=headers)
            self.assertEqual(referrer, actual.headers['Location'])
            actual = self.app.get('/logout', headers=headers)
            self.assertEqual(referrer, actual.headers['Location'])

        for host, referrer in [('localhost', 'http://example.com/productpage'),
                               ('localhost', 'http://localhost.example.com/productpage'),
                               ('localhost', 'javascript://localhost/%0aalert(1)'),
                               ('localhost', 'http://[::1'),
                               ('', 'http:example.com')]:
            headers = {'Host': host, 'Referer': referrer}
            actual = self.app.post('/login', data=data, headers=headers)
            self.assertEqual('productpage', actual.headers['Location'], referrer)
            actual = self.app.get('/logout', headers=headers)
            self.assertEqual('productpage', actual.headers['Location'], referrer)
