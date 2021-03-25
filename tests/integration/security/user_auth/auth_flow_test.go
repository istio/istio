// +build integ
// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package userauth

import (
	"net"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/tests/integration/security/user_auth/selenium"
	"istio.io/istio/tests/integration/security/user_auth/util"
)

func TestBasicAuthFlow(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.user.auth").
		Run(func(ctx framework.TestContext) {
			// check the port-forward availability
			validatePortForward(ctx, "8443")
			util.SetupConfig(ctx)

			// setup chrome and chromedriver
			service, wd := selenium.StartChromeOrFail(ctx)
			defer service.Stop()
			defer wd.Quit()

			// Navigate
			if err := wd.Get("https://localhost:8443/headers"); err != nil {
				ctx.Fatalf("unable to fetch the localhost headers page %v", err)
			}

			// Sign in email page
			if err := wd.WaitWithTimeout(selenium.WaitForTitleCondition("Sign in - Google Accounts"), 20*time.Second); err != nil {
				ctx.Fatalf("unable to load sign in page %v", err)
			}
			selenium.InputByXPathOrFail(ctx, wd, "//*[@id=\"identifierId\"]\n", "cloud_gatekeeper_prober_prod_authorized@gmail.com")
			selenium.ClickByXPathOrFail(ctx, wd, "//*[@id=\"identifierNext\"]/div/button")

			// Password page
			if err := wd.Wait(selenium.GoogleSignInPageIdleCondition()); err != nil {
				ctx.Fatalf("unable to load password page %v", err)
			}
			selenium.InputByCSSOrFail(ctx, wd, "#password input", "bB2iAGl7VfDE7n7")
			selenium.ClickByXPathOrFail(ctx, wd, "//*[@id=\"passwordNext\"]/div/button")

			// Headers page
			if err := wd.WaitWithTimeout(selenium.WaitForElementByXPathCondition("/html/body/pre"), 20*time.Second); err != nil {
				ctx.Fatalf("unable to load headers page %v", err)
			}

			// Get a reference to the text box containing code.
			elem := selenium.FindElementByXPathOrFail(ctx, wd, "/html/body/pre")
			tx, err := elem.Text()
			if err != nil {
				ctx.Fatalf("unable to get the text from headers page content %v", err)
			}
			ctx.Log(tx)
			if !strings.Contains(tx, "X-Asm-Rctoken") {
				ctx.Fatalf("X-Asm-Rctoken is not in the header")
			}

			// Refresh and should be able to access without login again
			ctx.Log("Refresh the page...")
			if err := wd.Refresh(); err != nil {
				ctx.Fatalf("unable to refresh the headers page %v", err)
			}
			if err := wd.WaitWithTimeout(selenium.WaitForElementByXPathCondition("/html/body/pre"), 20*time.Second); err != nil {
				ctx.Fatalf("unable to find content in headers page after refresh %v", err)
			}
			elem = selenium.FindElementByXPathOrFail(ctx, wd, "/html/body/pre")
			tx, err = elem.Text()
			if err != nil {
				ctx.Fatalf("unable to get the text from headers page content after refresh %v", err)
			}
			ctx.Log(tx)
			if !strings.Contains(tx, "X-Asm-Rctoken") {
				ctx.Fatalf("X-Asm-Rctoken is not in the header")
			}
			ctx.Log("Refresh finished.")
		})
}

func validatePortForward(ctx framework.TestContext, port string) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("localhost", port), time.Second)
	if err != nil {
		ctx.Fatalf("port-forward is not available: ", err)
	}
	if conn != nil {
		defer conn.Close()
		ctx.Logf("port-forward is available: ", net.JoinHostPort("localhost", "8443"))
	}
}
