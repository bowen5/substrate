# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Example environment variables for Substrate development on Azure/AKS.
# Copy this file to .ate-dev-env.sh and customize it for your environment:
#
#   cp hack/ate-azure-dev-env.sh.example .ate-dev-env.sh
#   source .ate-dev-env.sh

# Requires `az login` and a selected subscription.
export AZURE_SUBSCRIPTION_ID=$(az account show --query id -o tsv)

export AZURE_RESOURCE_GROUP=substrate-poc-rg
export AZURE_LOCATION=canadacentral

export AKS_CLUSTER_NAME=substrate-poc-aks
export AKS_KUBERNETES_VERSION=1.34.8
export AKS_NODE_POOL_NAME=systempool
export AKS_NODE_COUNT=2
export AKS_NODE_VM_SIZE=Standard_D4ads_v5
# Set these only if you need to override the defaults.
# export AKS_DNS_PREFIX=${AKS_CLUSTER_NAME}
# export AKS_VNET_SUBNET_ID=

# Storage account names are globally unique, 3-24 chars, lowercase letters/numbers only.
# Replace this placeholder with a globally unique value before provisioning.
export AZURE_STORAGE_ACCOUNT_NAME=substratepocstorage
export AZURE_STORAGE_CONTAINER_NAME=ate-snapshots
export AZURE_STORAGE_ACCOUNT_SKU=Standard_LRS

# Existing or separately-created Azure Container Registry used for ko images.
# Replace this placeholder with your ACR name before provisioning.
export AZURE_CONTAINER_REGISTRY_NAME=substratepocacr
export AZURE_CONTAINER_REGISTRY_RESOURCE_GROUP=${AZURE_RESOURCE_GROUP}

# Azure Workload Identity mapping for the atelet Kubernetes ServiceAccount.
export AZURE_ATELET_IDENTITY_RESOURCE_GROUP=${AZURE_RESOURCE_GROUP}
export AZURE_ATELET_IDENTITY_NAME=atelet
export AZURE_ATELET_KSA_NAMESPACE=ate-system
export AZURE_ATELET_KSA_NAME=atelet
# tools/setup-azure prints this value after creating/finding the identity.
# You can also fetch it manually:
#   az identity show --resource-group ${AZURE_ATELET_IDENTITY_RESOURCE_GROUP} --name ${AZURE_ATELET_IDENTITY_NAME} --query clientId -o tsv
export AZURE_ATELET_CLIENT_ID=908ef170-f9b6-45da-bbc0-287ccf0004e1
export AZURE_ATELET_FEDERATED_CREDENTIAL_NAME=${AZURE_ATELET_KSA_NAMESPACE}-${AZURE_ATELET_KSA_NAME}

# Select the AKS manifest overlay in hack/install-ate.sh.
export ATE_INSTALL_AKS=true

# Snapshot location for demo ActorTemplates on Azure Blob.
export SNAPSHOT_LOCATION=azblob://${AZURE_STORAGE_CONTAINER_NAME}/ate-demo-counter/

# ko image destination for AKS/ACR.
export KO_DOCKER_REPO=${AZURE_CONTAINER_REGISTRY_NAME}.azurecr.io/ate-images
export KO_DEFAULTPLATFORMS=linux/amd64
