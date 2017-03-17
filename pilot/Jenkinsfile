#!groovy

@Library('testutils@stable-afad32f')

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

  if (utils.runStage('PRESUBMIT')) {
    presubmit(gitUtils, bazel, utils)
  }

  if (utils.runStage('POSTSUBMIT')) {
    postsubmit(gitUtils, bazel, utils)
  }
}

def presubmit(gitUtils, bazel, utils) {
  goBuildNode(gitUtils, 'istio.io/manager') {
    bazel.updateBazelRc()
    utils.initTestingCluster()
    stage('Bazel Build') {
      // Use Testing cluster
      sh('ln -s ~/.kube/config platform/kube/')
      sh('bin/install-prereqs.sh')
      bazel.fetch('-k //...')
      bazel.build('//...')
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
      sh('bin/codecov.sh')
      utils.publishCodeCoverage('MANAGER_CODECOV_TOKEN')
    }
    stage('Integration Tests') {
      timeout(15) {
        sh('bin/e2e.sh -tag alpha' + gitUtils.GIT_SHA + ' -v 2')
      }
    }
  }
}

def postsubmit(gitUtils, bazel, utils) {
  buildNode(gitUtils) {
    stage('Docker Push') {
      bazel.updateBazelRc()
      def images = 'init,init_debug,app,app_debug,runtime,runtime_debug'
      def tags = "${gitUtils.GIT_SHORT_SHA},\$(date +%Y-%m-%d-%H.%M.%S),latest"
      utils.publishDockerImages(images, tags)
    }
    stage('Integration Tests') {
      timeout(30) {
        sh('bin/e2e.sh -count 10 -debug -tag alpha' + gitUtils.GIT_SHA + ' -v 2')
      }
    }
  }
}
