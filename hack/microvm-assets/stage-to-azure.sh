#!/usr/bin/env bash

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

# Stage the assembled micro-VM asset set into Azure Blob Storage under
# kata-assets/, where atelet fetches it via azblob://... URLs.
#
# Requires the `az` CLI authenticated with permissions to upload blobs.
# Env:
#   OUT                          asset dir, default ./bin/microvm-assets/amd64
#   AZURE_STORAGE_ACCOUNT_NAME   required
#   AZURE_STORAGE_CONTAINER_NAME required container name
#   AZURE_STORAGE_AUTH_MODE      az CLI auth mode, default key
#   AZURE_BLOB_PREFIX            object prefix, default kata-assets

set -o errexit -o nounset -o pipefail

ROOT="$(git rev-parse --show-toplevel)"

OUT="${OUT:-${ROOT}/bin/microvm-assets/amd64}"
ACCOUNT="${AZURE_STORAGE_ACCOUNT_NAME:-}"
CONTAINER="${AZURE_STORAGE_CONTAINER_NAME:-}"
AUTH_MODE="${AZURE_STORAGE_AUTH_MODE:-key}"
PREFIX="${AZURE_BLOB_PREFIX:-kata-assets}"

if [[ -z "${ACCOUNT}" ]]; then
  echo "error: AZURE_STORAGE_ACCOUNT_NAME is required" >&2
  exit 1
fi
if [[ -z "${CONTAINER}" ]]; then
  echo "error: AZURE_STORAGE_CONTAINER_NAME is required" >&2
  exit 1
fi

if ! command -v az >/dev/null 2>&1; then
  echo "error: the 'az' CLI is required but was not found in PATH" >&2
  exit 1
fi

for f in cloud-hypervisor vmlinux rootfs.img configuration-clh.toml; do
  if [[ ! -f "${OUT}/${f}" ]]; then
    echo "error: missing asset ${OUT}/${f}" >&2
    exit 1
  fi
done

if [[ -n "${AZURE_RESOURCE_GROUP:-}" ]]; then
  echo ">> Azure storage account:"
  az storage account show \
    --resource-group "${AZURE_RESOURCE_GROUP}" \
    --name "${ACCOUNT}" \
    --query '{name:name,kind:kind,sku:sku.name,isHnsEnabled:isHnsEnabled}' \
    -o table || true
fi

echo ">> Ensuring Azure Blob container ${ACCOUNT}/${CONTAINER} exists..."
az storage container create \
  --account-name "${ACCOUNT}" \
  --name "${CONTAINER}" \
  --auth-mode "${AUTH_MODE}" \
  --public-access off \
  >/dev/null

echo ">> Uploading assets to azblob://${CONTAINER}/${PREFIX}/ ..."
for f in cloud-hypervisor vmlinux rootfs.img configuration-clh.toml; do
  echo "   ${f}"
  az storage blob upload \
    --account-name "${ACCOUNT}" \
    --container-name "${CONTAINER}" \
    --name "${PREFIX}/${f}" \
    --file "${OUT}/${f}" \
    --auth-mode "${AUTH_MODE}" \
    --overwrite true \
    >/dev/null
done

echo ">> Done. Verify:"
az storage blob list \
  --account-name "${ACCOUNT}" \
  --container-name "${CONTAINER}" \
  --prefix "${PREFIX}/" \
  --auth-mode "${AUTH_MODE}" \
  --query '[].{name:name, size:properties.contentLength}' \
  -o table
