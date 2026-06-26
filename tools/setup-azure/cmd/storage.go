// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
)

func createSnapshotStorage(ctx context.Context, env *Environment) error {
	storageEnv, err := requireSnapshotStorageEnv()
	if err != nil {
		return err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("create Azure default credential: %w", err)
	}

	resourceGroupsClient, err := armresources.NewResourceGroupsClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure resource groups client: %w", err)
	}

	accountsClient, err := armstorage.NewAccountsClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure storage accounts client: %w", err)
	}

	containersClient, err := armstorage.NewBlobContainersClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure blob containers client: %w", err)
	}

	if err := createResourceGroup(ctx, resourceGroupsClient, storageEnv.ResourceGroup, storageEnv.Location); err != nil {
		return err
	}

	if err := createStorageAccountIdempotent(ctx, accountsClient, storageEnv); err != nil {
		return err
	}

	return createBlobContainerIdempotent(ctx, containersClient, storageEnv)
}

func createStorageAccountIdempotent(ctx context.Context, client *armstorage.AccountsClient, env *SnapshotStorageEnvironment) error {
	slog.Info("Checking if storage account exists", slog.String("resourceGroup", env.ResourceGroup), slog.String("account", env.AccountName))
	accountResp, err := client.GetProperties(ctx, env.ResourceGroup, env.AccountName, nil)
	if err != nil {
		if !isNotFound(err) {
			return fmt.Errorf("get storage account %s: %w", env.AccountName, err)
		}
		return createStorageAccountInternal(ctx, client, env)
	}

	return validateExistingStorageAccount(accountResp.Account, env)
}

func createStorageAccountInternal(ctx context.Context, client *armstorage.AccountsClient, env *SnapshotStorageEnvironment) error {
	slog.Info("Storage account does not exist. Creating...", slog.String("resourceGroup", env.ResourceGroup), slog.String("account", env.AccountName), slog.String("location", env.Location))

	accountKind := storageAccountKind(env.SKUName)
	accessTier := storageAccountAccessTier(env.SKUName)

	createCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	poller, err := client.BeginCreate(createCtx, env.ResourceGroup, env.AccountName, armstorage.AccountCreateParameters{
		Location: to.Ptr(env.Location),
		Kind:     to.Ptr(accountKind),
		SKU: &armstorage.SKU{
			Name: to.Ptr(armstorage.SKUName(env.SKUName)),
		},
		Properties: &armstorage.AccountPropertiesCreateParameters{
			AccessTier:             to.Ptr(accessTier),
			AllowBlobPublicAccess:  to.Ptr(false),
			EnableHTTPSTrafficOnly: to.Ptr(true),
			MinimumTLSVersion:      to.Ptr(armstorage.MinimumTLSVersionTLS12),
			PublicNetworkAccess:    to.Ptr(armstorage.PublicNetworkAccessEnabled),
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("begin create storage account %s: %w", env.AccountName, err)
	}

	_, err = poller.PollUntilDone(createCtx, &runtime.PollUntilDoneOptions{Frequency: 10 * time.Second})
	if err != nil {
		return fmt.Errorf("create storage account %s: %w", env.AccountName, err)
	}

	slog.Info("Storage account created", slog.String("resourceGroup", env.ResourceGroup), slog.String("account", env.AccountName))
	return nil
}

func validateExistingStorageAccount(account armstorage.Account, env *SnapshotStorageEnvironment) error {
	slog.Info("Storage account exists. Checking attributes...", slog.String("account", env.AccountName))

	if account.Location != nil && !strings.EqualFold(*account.Location, env.Location) {
		return fmt.Errorf("storage account %s is in location %s, but expected %s", env.AccountName, *account.Location, env.Location)
	}
	expectedKind := storageAccountKind(env.SKUName)
	if account.Kind != nil && *account.Kind != expectedKind {
		return fmt.Errorf("storage account %s has kind %s, but expected %s", env.AccountName, *account.Kind, expectedKind)
	}
	if account.SKU != nil && account.SKU.Name != nil && !strings.EqualFold(string(*account.SKU.Name), env.SKUName) {
		slog.Warn("Storage account SKU differs from requested SKU; leaving existing account unchanged", slog.String("account", env.AccountName), slog.String("current", string(*account.SKU.Name)), slog.String("requested", env.SKUName))
	}

	slog.Info("Storage account attributes look compatible", slog.String("account", env.AccountName))
	return nil
}

func createBlobContainerIdempotent(ctx context.Context, client *armstorage.BlobContainersClient, env *SnapshotStorageEnvironment) error {
	slog.Info("Checking if blob container exists", slog.String("account", env.AccountName), slog.String("container", env.ContainerName))
	_, err := client.Get(ctx, env.ResourceGroup, env.AccountName, env.ContainerName, nil)
	if err != nil {
		if !isNotFound(err) {
			return fmt.Errorf("get blob container %s: %w", env.ContainerName, err)
		}

		slog.Info("Blob container does not exist. Creating...", slog.String("account", env.AccountName), slog.String("container", env.ContainerName))
		_, err = client.Create(ctx, env.ResourceGroup, env.AccountName, env.ContainerName, armstorage.BlobContainer{
			ContainerProperties: &armstorage.ContainerProperties{
				PublicAccess: to.Ptr(armstorage.PublicAccessNone),
			},
		}, nil)
		if err != nil {
			return fmt.Errorf("create blob container %s: %w", env.ContainerName, err)
		}
		slog.Info("Blob container created", slog.String("account", env.AccountName), slog.String("container", env.ContainerName))
		return nil
	}

	slog.Info("Blob container already exists", slog.String("account", env.AccountName), slog.String("container", env.ContainerName))
	return nil
}

func storageAccountKind(skuName string) armstorage.Kind {
	if isPremiumBlobSKU(skuName) {
		return armstorage.KindBlockBlobStorage
	}
	return armstorage.KindStorageV2
}

func storageAccountAccessTier(skuName string) armstorage.AccessTier {
	if isPremiumBlobSKU(skuName) {
		return armstorage.AccessTierPremium
	}
	return armstorage.AccessTierHot
}

func isPremiumBlobSKU(skuName string) bool {
	return strings.EqualFold(skuName, string(armstorage.SKUNamePremiumLRS)) || strings.EqualFold(skuName, string(armstorage.SKUNamePremiumZRS))
}
