// Copyright 2016 Circonus, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package checkmgr

import (
	"github.com/circonus-labs/circonus-gometrics/api"
)

// IsMetricActive checks whether a given metric name is currently active(enabled)
func (cm *CheckManager) IsMetricActive(name string) bool {
	cm.availableMetricsmu.Lock()
	defer cm.availableMetricsmu.Unlock()

	return cm.availableMetrics[name]
}

// ActivateMetric determines if a given metric should be activated
func (cm *CheckManager) ActivateMetric(name string) bool {
	cm.availableMetricsmu.Lock()
	defer cm.availableMetricsmu.Unlock()

	active, exists := cm.availableMetrics[name]

	if !exists {
		return true
	}

	if !active && cm.forceMetricActivation {
		return true
	}

	return false
}

// AddMetricTags updates check bundle metrics with tags
func (cm *CheckManager) AddMetricTags(metricName string, tags []string, appendTags bool) bool {
	tagsUpdated := false

	if appendTags && len(tags) == 0 {
		return tagsUpdated
	}

	currentTags, exists := cm.metricTags[metricName]
	if !exists {
		foundMetric := false

		if cm.checkBundle != nil {
			for _, metric := range cm.checkBundle.Metrics {
				if metric.Name == metricName {
					foundMetric = true
					currentTags = metric.Tags
					break
				}
			}
		}

		if !foundMetric {
			currentTags = []string{}
		}
	}

	action := ""
	if appendTags {
		numNewTags := countNewTags(currentTags, tags)
		if numNewTags > 0 {
			action = "Added"
			currentTags = append(currentTags, tags...)
			tagsUpdated = true
		}
	} else {
		if len(tags) != len(currentTags) {
			action = "Set"
			currentTags = tags
			tagsUpdated = true
		} else {
			numNewTags := countNewTags(currentTags, tags)
			if numNewTags > 0 {
				action = "Set"
				currentTags = tags
				tagsUpdated = true
			}
		}
	}

	if tagsUpdated {
		cm.metricTags[metricName] = currentTags
	}

	if cm.Debug && action != "" {
		cm.Log.Printf("[DEBUG] %s metric tag(s) %s %v\n", action, metricName, tags)
	}

	return tagsUpdated
}

// addNewMetrics updates a check bundle with new metrics
func (cm *CheckManager) addNewMetrics(newMetrics map[string]*api.CheckBundleMetric) bool {
	updatedCheckBundle := false

	if cm.checkBundle == nil || len(newMetrics) == 0 {
		return updatedCheckBundle
	}

	cm.cbmu.Lock()
	defer cm.cbmu.Unlock()

	numCurrMetrics := len(cm.checkBundle.Metrics)
	numNewMetrics := len(newMetrics)

	if numCurrMetrics+numNewMetrics >= cap(cm.checkBundle.Metrics) {
		nm := make([]api.CheckBundleMetric, numCurrMetrics+numNewMetrics)
		copy(nm, cm.checkBundle.Metrics)
		cm.checkBundle.Metrics = nm
	}

	cm.checkBundle.Metrics = cm.checkBundle.Metrics[0 : numCurrMetrics+numNewMetrics]

	i := 0
	for _, metric := range newMetrics {
		cm.checkBundle.Metrics[numCurrMetrics+i] = *metric
		i++
		updatedCheckBundle = true
	}

	if updatedCheckBundle {
		cm.forceCheckUpdate = true
	}

	return updatedCheckBundle
}

// inventoryMetrics creates list of active metrics in check bundle
func (cm *CheckManager) inventoryMetrics() {
	availableMetrics := make(map[string]bool)
	for _, metric := range cm.checkBundle.Metrics {
		availableMetrics[metric.Name] = metric.Status == "active"
	}
	cm.availableMetricsmu.Lock()
	cm.availableMetrics = availableMetrics
	cm.availableMetricsmu.Unlock()
}

// countNewTags returns a count of new tags which do not exist in the current list of tags
func countNewTags(currTags []string, newTags []string) int {
	if len(newTags) == 0 {
		return 0
	}

	if len(currTags) == 0 {
		return len(newTags)
	}

	newTagCount := 0

	for _, newTag := range newTags {
		found := false
		for _, currTag := range currTags {
			if newTag == currTag {
				found = true
				break
			}
		}
		if !found {
			newTagCount++
		}
	}

	return newTagCount
}
