package prow

import (
	"fmt"
	"path/filepath"

	"istio.io/pkg/env"
)

var (
	runningInCI = env.RegisterBoolVar("CI", false, "If true, indicates we are running in CI").Get()
	artifactsBase = env.RegisterStringVar("PROW_ARTIFACTS_BASE", "https://gcsweb.istio.io/gcs/istio-prow", "the base url for prow artifacts").Get()
	// https://github.com/kubernetes/test-infra/blob/master/prow/jobs.md#job-environment-variables
	jobType = env.RegisterStringVar("JOB_TYPE", "presubmit", "type of job").Get()
	jobName = env.RegisterStringVar("JOB_NAME", "", "name of job").Get()
	pullNumber = env.RegisterStringVar("PULL_NUMBER", "", "PR of job").Get()
	repoName = env.RegisterStringVar("REPO_NAME", "istio", "repo name").Get()
	repoOwner = env.RegisterStringVar("REPO_OWNER", "istio", "repo owner").Get()
	buildID = env.RegisterStringVar("BUILD_ID", "", "build id").Get()
)

func ArtifactsURL(filename string) string {
	if !runningInCI {
		return filename
	}
	if jobType == "presubmit" {
		return filepath.Join(artifactsBase, "pr-logs/pull", fmt.Sprintf("%s_%s", repoOwner, repoName), pullNumber, jobName, buildID, filename)
	}
	return filepath.Join(artifactsBase, "logs", jobName, buildID, filename)
}
