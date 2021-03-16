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

// Package selenium contains all the util functions that are abstracted above tebeka/selenium
package selenium

import (
	"fmt"
	"os"

	"github.com/tebeka/selenium"
	"github.com/tebeka/selenium/chrome"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
)

const (
	dependenciesPath = "../../../../prow/asm/tester/configs/user-auth/dependencies/"
	seleniumPath     = dependenciesPath + "selenium-server.jar"
	chromeDriverPath = dependenciesPath + "chromedriver_linux64/chromedriver"
	chromePath       = dependenciesPath + "chrome-linux/chrome"
	userAgent        = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.82 Safari/537.36"
	port             = 8080
)

// StartChromeOrFail setup Chrome and ChromeDriver
func StartChromeOrFail(ctx framework.TestContext) (*selenium.Service, selenium.WebDriver) {
	ctx.Helper()
	opts := []selenium.ServiceOption{
		selenium.ChromeDriver(chromeDriverPath),
		selenium.Output(os.Stderr), // Output debug information to STDERR.
	}
	selenium.SetDebug(true)
	service, err := selenium.NewSeleniumService(seleniumPath, port, opts...)
	if err != nil {
		ctx.Fatal(err)
	}

	// Connect to the WebDriver instance running locally.
	caps := selenium.Capabilities{"browserName": "chrome"}
	chrCaps := chrome.Capabilities{
		Path: chromePath,
		Args: []string{
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--headless",
			"--ignore-certificate-errors",
			"--user-agent=" + userAgent,
		},
		W3C: true,
	}
	caps.AddChrome(chrCaps)

	wd, err := selenium.NewRemote(caps, fmt.Sprintf("http://localhost:%d/wd/hub", port))
	if err != nil {
		ctx.Fatal(err)
	}

	return service, wd
}

// GoogleSignInPageIdleCondition condition to wait accounts.google.com root view element to be idle
func GoogleSignInPageIdleCondition() selenium.Condition {
	return func(wd selenium.WebDriver) (bool, error) {
		element, err := wd.FindElement(selenium.ByXPATH, "//*[@id=\"initialView\"]")
		if err != nil {
			return false, nil
		}
		_, err = element.GetAttribute("aria-busy")
		if err == nil {
			return false, nil
		}

		return err.Error() == "nil return value", nil
	}
}

// ClickByXPathOrFail click web element found by XPath or fail
func ClickByXPathOrFail(t test.Failer, wd selenium.WebDriver, value string) {
	clickOrFail(t, wd, selenium.ByXPATH, value)
}

func clickOrFail(t test.Failer, wd selenium.WebDriver, by, value string) {
	element, err := wd.FindElement(by, value)
	if err != nil {
		t.Fatalf("unable to find element %s: %v", value, err)
	}
	if err := element.Click(); err != nil {
		t.Fatalf("unable to click element %s: %v", value, err)
	}
}

// InputByXPathOrFail input value into web element found by XPath or fail
func InputByXPathOrFail(t test.Failer, wd selenium.WebDriver, value string, input string) {
	inputOrFail(t, wd, selenium.ByXPATH, value, input)
}

// InputByXPathOrFail input value into web element found by CSS selector or fail
func InputByCSSOrFail(t test.Failer, wd selenium.WebDriver, value string, input string) {
	inputOrFail(t, wd, selenium.ByCSSSelector, value, input)
}

func inputOrFail(t test.Failer, wd selenium.WebDriver, by, value string, input string) {
	element, err := wd.FindElement(by, value)
	if err != nil {
		t.Fatalf("unable to find element %s: %v", value, err)
	}
	if element == nil {
		t.Fatalf("found nil element: %s", value)
	}
	if err := element.Clear(); err != nil {
		t.Fatalf("unable to clear the input field %s: %v", value, err)
	}
	if err := element.SendKeys(input); err != nil {
		t.Fatalf("unable to input to %s: %v", value, err)
	}
}

// WaitForTitleCondition condition to wait util title be loaded
func WaitForTitleCondition(newTitle string) selenium.Condition {
	return func(wd selenium.WebDriver) (bool, error) {
		title, err := wd.Title()
		if err != nil {
			return false, err
		}
		return title == newTitle, nil
	}
}

// WaitForElementByXPathCondition condition to wait until web element found by XPath to be loaded
func WaitForElementByXPathCondition(value string) selenium.Condition {
	return waitForElementCondition(selenium.ByXPATH, value)
}

func waitForElementCondition(by, value string) selenium.Condition {
	return func(wd selenium.WebDriver) (bool, error) {
		_, err := wd.FindElement(by, value)
		if err != nil {
			return false, nil
		}
		return true, nil
	}
}

// FindElementByXPathOrFail find web element by XPath or fail
func FindElementByXPathOrFail(t test.Failer, wd selenium.WebDriver, value string) selenium.WebElement {
	elem, err := wd.FindElement(selenium.ByXPATH, value)
	if err != nil {
		t.Fatalf("unable to find element by XPATH %s: %v", value, err)
	}
	return elem
}
