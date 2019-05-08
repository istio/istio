// Copyright 2016 Circonus, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package checkmgr

import (
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/circonus-labs/circonus-gometrics/api"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Get Broker to use when creating a check
func (cm *CheckManager) getBroker() (*api.Broker, error) {
	if cm.brokerID != 0 {
		cid := fmt.Sprintf("/broker/%d", cm.brokerID)
		broker, err := cm.apih.FetchBroker(api.CIDType(&cid))
		if err != nil {
			return nil, err
		}
		if !cm.isValidBroker(broker) {
			return nil, fmt.Errorf(
				"[ERROR] designated broker %d [%s] is invalid (not active, does not support required check type, or connectivity issue)",
				cm.brokerID,
				broker.Name)
		}
		return broker, nil
	}
	broker, err := cm.selectBroker()
	if err != nil {
		return nil, fmt.Errorf("[ERROR] Unable to fetch suitable broker %s", err)
	}
	return broker, nil
}

// Get CN of Broker associated with submission_url to satisfy no IP SANS in certs
func (cm *CheckManager) getBrokerCN(broker *api.Broker, submissionURL api.URLType) (string, error) {
	u, err := url.Parse(string(submissionURL))
	if err != nil {
		return "", err
	}

	hostParts := strings.Split(u.Host, ":")
	host := hostParts[0]

	if net.ParseIP(host) == nil { // it's a non-ip string
		return u.Host, nil
	}

	cn := ""

	for _, detail := range broker.Details {
		if *detail.IP == host {
			cn = detail.CN
			break
		}
	}

	if cn == "" {
		return "", fmt.Errorf("[ERROR] Unable to match URL host (%s) to Broker", u.Host)
	}

	return cn, nil

}

// Select a broker for use when creating a check, if a specific broker
// was not specified.
func (cm *CheckManager) selectBroker() (*api.Broker, error) {
	var brokerList *[]api.Broker
	var err error
	enterpriseType := "enterprise"

	if len(cm.brokerSelectTag) > 0 {
		filter := api.SearchFilterType{
			"f__tags_has": cm.brokerSelectTag,
		}
		brokerList, err = cm.apih.SearchBrokers(nil, &filter)
		if err != nil {
			return nil, err
		}
	} else {
		brokerList, err = cm.apih.FetchBrokers()
		if err != nil {
			return nil, err
		}
	}

	if len(*brokerList) == 0 {
		return nil, fmt.Errorf("zero brokers found")
	}

	validBrokers := make(map[string]api.Broker)
	haveEnterprise := false

	for _, broker := range *brokerList {
		broker := broker
		if cm.isValidBroker(&broker) {
			validBrokers[broker.CID] = broker
			if broker.Type == enterpriseType {
				haveEnterprise = true
			}
		}
	}

	if haveEnterprise { // eliminate non-enterprise brokers from valid brokers
		for k, v := range validBrokers {
			if v.Type != enterpriseType {
				delete(validBrokers, k)
			}
		}
	}

	if len(validBrokers) == 0 {
		return nil, fmt.Errorf("found %d broker(s), zero are valid", len(*brokerList))
	}

	validBrokerKeys := reflect.ValueOf(validBrokers).MapKeys()
	selectedBroker := validBrokers[validBrokerKeys[rand.Intn(len(validBrokerKeys))].String()]

	if cm.Debug {
		cm.Log.Printf("[DEBUG] Selected broker '%s'\n", selectedBroker.Name)
	}

	return &selectedBroker, nil

}

// Verify broker supports the check type to be used
func (cm *CheckManager) brokerSupportsCheckType(checkType CheckTypeType, details *api.BrokerDetail) bool {

	baseType := string(checkType)

	for _, module := range details.Modules {
		if module == baseType {
			return true
		}
	}

	if idx := strings.Index(baseType, ":"); idx > 0 {
		baseType = baseType[0:idx]
	}

	for _, module := range details.Modules {
		if module == baseType {
			return true
		}
	}

	return false

}

// Is the broker valid (active, supports check type, and reachable)
func (cm *CheckManager) isValidBroker(broker *api.Broker) bool {
	var brokerHost string
	var brokerPort string

	if broker.Type != "circonus" && broker.Type != "enterprise" {
		return false
	}

	valid := false

	for _, detail := range broker.Details {
		detail := detail

		// broker must be active
		if detail.Status != statusActive {
			if cm.Debug {
				cm.Log.Printf("[DEBUG] Broker '%s' is not active.\n", broker.Name)
			}
			continue
		}

		// broker must have module loaded for the check type to be used
		if !cm.brokerSupportsCheckType(cm.checkType, &detail) {
			if cm.Debug {
				cm.Log.Printf("[DEBUG] Broker '%s' does not support '%s' checks.\n", broker.Name, cm.checkType)
			}
			continue
		}

		if detail.ExternalPort != 0 {
			brokerPort = strconv.Itoa(int(detail.ExternalPort))
		} else {
			if detail.Port != nil && *detail.Port != 0 {
				brokerPort = strconv.Itoa(int(*detail.Port))
			} else {
				brokerPort = "43191"
			}
		}

		if detail.ExternalHost != nil && *detail.ExternalHost != "" {
			brokerHost = *detail.ExternalHost
		} else if detail.IP != nil && *detail.IP != "" {
			brokerHost = *detail.IP
		}

		if brokerHost == "" {
			cm.Log.Printf("[WARN] Broker '%s' instance %s has no IP or external host set", broker.Name, detail.CN)
			continue
		}

		if brokerHost == "trap.noit.circonus.net" && brokerPort != "443" {
			brokerPort = "443"
		}

		retries := 5
		for attempt := 1; attempt <= retries; attempt++ {
			// broker must be reachable and respond within designated time
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%s", brokerHost, brokerPort), cm.brokerMaxResponseTime)
			if err == nil {
				conn.Close()
				valid = true
				break
			}

			cm.Log.Printf("[WARN] Broker '%s' unable to connect, %v. Retrying in 2 seconds, attempt %d of %d.", broker.Name, err, attempt, retries)
			time.Sleep(2 * time.Second)
		}

		if valid {
			if cm.Debug {
				cm.Log.Printf("[DEBUG] Broker '%s' is valid\n", broker.Name)
			}
			break
		}
	}
	return valid
}
