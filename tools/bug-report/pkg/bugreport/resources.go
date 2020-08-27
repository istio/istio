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

package bugreport

const (
	bugReportHelpKubeconfig     = "Path to kube config."
	bugReportHelpContext        = "Name of the kubeconfig Context to use."
	bugReportHelpFilename       = "Path to a file containing configuration in YAML format."
	bugReportHelpIstioNamespace = "List of comma-separated namespaces where Istio control planes " +
		"are installed."
	bugReportHelpDryRun         = "Console output only, does not actually capture logs."
	bugReportHelpCommandTimeout = "Maximum amount of time to spend fetching logs. When timeout is reached " +
		"only the logs captured so far are saved to the archive."
	bugReportHelpMaxArchiveSizeMb = "Maximum size of the compressed archive in Mb. Logs are prioritized" +
		"according to importance heuristics."
	bugReportHelpInclude = "Spec for which pods' proxy logs to include in the archive. See 'help' for examples."
	bugReportHelpExclude = "Spec for which pods' proxy logs to exclude from the archive, after the include spec " +
		"is processed. See 'help' for examples."
	bugReportHelpStartTime = "Start time for the range of log entries to include in the archive. " +
		"Default is the infinite past. If set, Since must be unset."
	bugReportHelpEndTime = "End time for the range of log entries to include in the archive. Default is now."
	bugReportHelpSince   = "How far to go back in time from end-time for log entries to include in the archive. " +
		"Default is infinity. If set, start-time must be unset."
	bugReportHelpCriticalErrors = "List of comma separated glob patters to match against log error strings. " +
		"If any pattern matches an error in the log, the logs is given the highest priority for archive inclusion."
	bugReportHelpIgnoredErrors = "List of comma separated glob patters to match against log error strings. " +
		"Any error matching these patters is ignored when calculating the log importance heuristic."
	bugReportHelpGCSURL      = "URL of the GCS bucket where the archive is uploaded."
	bugReportHelpUploadToGCS = "Upload archive to GCS bucket. If gcs-url is unset, a new bucket is created."
	bugReportHelpTempDir     = "Set a specific directory for temporary artifact storage."
)
