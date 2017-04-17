#!groovy

@Library('testutils@stable-cd138c4')

import org.istio.testutils.Utilities
import org.istio.testutils.GitUtilities
import org.istio.testutils.Bazel

// Utilities shared amongst modules
def gitUtils = new GitUtilities()
def utils = new Utilities()
def bazel = new Bazel()

mainFlow(utils) {
  node {
    gitUtils.initialize()
    bazel.setVars()
  }
  // PR on master branch
  if (utils.runStage('PRESUBMIT')) {
    presubmit(gitUtils, bazel, utils)
  }
  // Postsubmit from master branch
  if (utils.runStage('POSTSUBMIT')) {
    postsubmit(gitUtils, bazel, utils)
  }
  // PR from master to stable branch for qualification
  if (utils.runStage('STABLE_PRESUBMIT')) {
    stablePresubmit(gitUtils, bazel, utils)
  }
  // Postsubmit form stable branch, post qualification
  if (utils.runStage('STABLE_POSTSUBMIT')) {
    stablePostsubmit(gitUtils, bazel, utils)
  }
  // Regression test to run for modules managed depends on
  if (utils.runStage('REGRESSION')) {
    managerRegression(gitUtils, bazel, utils)
  }
}

def presubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/manager') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    sh('ln -s ~/.kube/config platform/kube/')
    stage('Bazel Build') {
      // Use Testing cluster
      sh('bin/install-prereqs.sh')
      bazel.fetch('-k //...')
      bazel.build('//...')
    }
    stage('Build istioctl') {
      def remotePath = gitUtils.artifactsPath('istioctl')
      sh("bin/cross-compile-istioctl -p ${remotePath}")
    }
    stage('Go Build') {
      sh('bin/init.sh')
    }
    stage('Code Check') {
      sh('bin/check.sh')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Code Coverage') {
      sh('bin/codecov.sh > codecov.report')
      sh('bazel-bin/bin/toolbox/presubmit/package_coverage_check')
      utils.publishCodeCoverage('MANAGER_CODECOV_TOKEN')
    }
    stage('Integration Tests') {
      timeout(15) {
        sh("bin/e2e.sh -tag ${env.GIT_SHA} -v 2")
      }
    }
  }
}

def stablePresubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/manager') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    sh('ln -s ~/.kube/config platform/kube/')
    stage('Build istioctl') {
      def remotePath = gitUtils.artifactsPath('istioctl')
      sh("bin/cross-compile-istioctl -p ${remotePath}")
    }
    stage('Integration Tests') {
      timeout(30) {
        sh("bin/e2e.sh -count 10 -logs=false -tag ${env.GIT_SHA}")
      }
    }
  }
}

def stablePostsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/manager') {
    bazel.updateBazelRc()
    sh('touch platform/kube/config')
    stage('Build istioctl') {
      def remotePath = gitUtils.artifactsPath('istioctl')
      sh("bin/cross-compile-istioctl -p ${remotePath}")
    }
    stage('Docker Push') {
      def date = new Date().format("YYYY-MM-dd-HH.mm.ss")
      def images = 'init,init_debug,app,app_debug,proxy,proxy_debug,manager,manager_debug'
      def tags = "${env.GIT_SHORT_SHA},${date},latest"
      utils.publishDockerImagesToDockerHub(images, tags)
      utils.publishDockerImagesToContainerRegistry(images, tags, '', 'gcr.io/istio-io')
    }
  }
}

def postsubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/manager') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    sh('ln -s ~/.kube/config platform/kube/')
    stage('Code Coverage') {
      sh('bin/install-prereqs.sh')
      bazel.test('//...')
      sh('bin/init.sh')
      sh('bin/codecov.sh')
      utils.publishCodeCoverage('MANAGER_CODECOV_TOKEN')
    }
    utils.fastForwardStable('manager')
  }
}

def managerRegression(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/manager') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Bazel Build') {
      // Use Testing cluster
      sh('ln -s ~/.kube/config platform/kube/')
      bazel.fetch('-k //...')
      bazel.build('//...')
    }
    stage('Bazel Tests') {
      bazel.test('//...')
    }
    stage('Integration Tests') {
      timeout(15) {
        sh("bin/e2e.sh -tag ${env.GIT_SHA}")
      }
    }
  }
}
