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

package nft

import (
	"fmt"

	"sigs.k8s.io/knftables"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/common/config"
	"istio.io/istio/tools/common/tproxy"
	"istio.io/istio/tools/istio-nftables/pkg/capture"
)

// ProgramNftables sets up nftables rules for traffic redirection.
// It also sets up TPROXY rules if rules are successfully applied.
func ProgramNftables(cfg *config.Config) error {
	log.Info("native nftables enabled, using nft rules for traffic redirection.")

	if !cfg.SkipRuleApply {
		nftConfigurator, err := capture.NewNftablesConfigurator(cfg, nil)
		if err != nil {
			return err
		}

		rules, err := nftConfigurator.Run()
		if err != nil {
			return err
		}

		if err := tproxy.ConfigureRoutes(cfg); err != nil {
			return fmt.Errorf("failed to configure routes: %v", err)
		}
		logNftRules(rules)
	}
	return nil
}

func logNftRules(rules *knftables.Transaction) {
	if rules.NumOperations() == 0 {
		log.Infof("There are no nftables rules to log")
		return
	}

	nftProvider, err := capture.NewNftImpl("", "")
	if err != nil {
		log.Errorf("Error creating NftImpl interface: %v", err)
		return
	}

	dump := nftProvider.Dump(rules)
	if dump != "" {
		log.Infof("nftables rules programmed:\n%s \n", dump)
	} else {
		log.Info("There are no nftables rules.")
	}
}
