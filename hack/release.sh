#!/usr/bin/env bash

# standard bash error handling
set -o nounset  # treat unset variables as an error and exit immediately.
set -o errexit  # exit immediately when a command fails.
set -E          # needs to be set if we want the ERR trap
set -o pipefail # prevents errors in a pipeline from being masked

readonly MODULE_REGISTRY="europe-docker.pkg.dev/kyma-project/prod/unsigned"
readonly GCP_ACCESS_TOKEN=$(gcloud auth application-default print-access-token)

function create_module() {
    echo "Creating the module"
    ${KUSTOMIZE} build config/default > telemetry-manager.yaml
    ${KYMA} alpha create module \
    --module-config-file=module-config.yaml \
    --registry ${MODULE_REGISTRY} \
    --credentials oauth2accesstoken:${GCP_ACCESS_TOKEN} \
    --output moduletemplate.yaml \
    --module-archive-version-overwrite \
    --ci
}

function create_dev_module() {
    echo "Creating the development module"
    ${KUSTOMIZE} build config/development > telemetry-manager-dev.yaml
    ${KYMA} alpha create module \
    --module-config-file=module-config-dev.yaml \
    --registry ${MODULE_REGISTRY} \
    --credentials oauth2accesstoken:${GCP_ACCESS_TOKEN} \
    --output moduletemplate-dev.yaml \
    --module-archive-version-overwrite \
    --ci
}

function create_github_release() {
    echo "Creating the Github release"
    # rename the file for Telemetry default CR to have a better naming as a release artefact
    cp ./config/samples/operator_v1alpha1_telemetry.yaml telemetry-default-cr.yaml
    git reset --hard
    curl -sL https://git.io/goreleaser | VERSION=${GORELEASER_VERSION} bash
}

function main() {
    # Adding the remote repo is needed for the kyma alpha create module command and for goreleaser
    git remote add origin https://github.com/kyma-project/telemetry-manager

    cd config/manager && ${KUSTOMIZE} edit set image controller=${IMG} && cd ../..

    # Create the module and push its image to the prod registry defined in MODULE_REGISTRY
    create_module

    # Create the dev module for the experimental channel and push its image to the prod registry defined in MODULE_REGISTRY
    create_dev_module

    # Create github release entry using goreleaser
    create_github_release
}

main
