Syncronizing with .circleci
---------------------------


1. CI config

There should be matching targets between .circleci/config.yml and
.jenkins/Jenkinsfile. Jenkins has flattened the dependency tree defined for
circleci. 

2. Builder images

./.circleci/Dockerfile and ./.jenkins/Dockerfile.jenkins-build should be roughly the same

./.jenkins/Dockerfile.minikube is used to replicate the minikube environment that 
