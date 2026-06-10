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

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/google/uuid"
)

const (
	azureADTokenExchangeAudience     = "api://AzureADTokenExchange"
	storageBlobDataContributorRoleID = "ba92f5b4-2d11-453d-a403-e96b0029c9fe"
)

func createAteletWorkloadIdentity(ctx context.Context, env *Environment) error {
	identityEnv, err := requireAteletWorkloadIdentityEnv()
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

	clustersClient, err := armcontainerservice.NewManagedClustersClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure managed clusters client: %w", err)
	}

	identitiesClient, err := armmsi.NewUserAssignedIdentitiesClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure user assigned identities client: %w", err)
	}

	federatedCredsClient, err := armmsi.NewFederatedIdentityCredentialsClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure federated identity credentials client: %w", err)
	}

	if err := createResourceGroup(ctx, resourceGroupsClient, identityEnv.IdentityResourceGroup, identityEnv.Location); err != nil {
		return err
	}

	_, err = ensureAteletWorkloadIdentity(ctx, clustersClient, identitiesClient, federatedCredsClient, identityEnv)
	return err
}

func grantAteletPermissions(ctx context.Context, env *Environment) error {
	permissionsEnv, err := requireAteletPermissionsEnv()
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

	clustersClient, err := armcontainerservice.NewManagedClustersClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure managed clusters client: %w", err)
	}

	identitiesClient, err := armmsi.NewUserAssignedIdentitiesClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure user assigned identities client: %w", err)
	}

	federatedCredsClient, err := armmsi.NewFederatedIdentityCredentialsClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure federated identity credentials client: %w", err)
	}

	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure role assignments client: %w", err)
	}

	if err := createResourceGroup(ctx, resourceGroupsClient, permissionsEnv.IdentityResourceGroup, permissionsEnv.Location); err != nil {
		return err
	}

	principalID, err := ensureAteletWorkloadIdentity(ctx, clustersClient, identitiesClient, federatedCredsClient, &permissionsEnv.AteletWorkloadIdentityEnvironment)
	if err != nil {
		return err
	}

	if err := createSnapshotStorageRoleAssignmentIdempotent(ctx, roleAssignmentsClient, env.SubscriptionID, permissionsEnv, principalID); err != nil {
		return err
	}
	return createAteletAcrPullRoleAssignmentIdempotent(ctx, roleAssignmentsClient, env.SubscriptionID, permissionsEnv, principalID)
}

func ensureAteletWorkloadIdentity(ctx context.Context, clustersClient *armcontainerservice.ManagedClustersClient, identitiesClient *armmsi.UserAssignedIdentitiesClient, federatedCredsClient *armmsi.FederatedIdentityCredentialsClient, env *AteletWorkloadIdentityEnvironment) (string, error) {
	issuerURL, err := aksOIDCIssuerURL(ctx, clustersClient, env)
	if err != nil {
		return "", err
	}

	identity, err := createAteletIdentityIdempotent(ctx, identitiesClient, env)
	if err != nil {
		return "", err
	}

	principalID, err := userAssignedIdentityPrincipalID(identity, env.IdentityName)
	if err != nil {
		return "", err
	}
	clientID, err := userAssignedIdentityClientID(identity, env.IdentityName)
	if err != nil {
		return "", err
	}
	slog.Info("Atelet managed identity client ID", slog.String("identity", env.IdentityName), slog.String("clientID", clientID))
	fmt.Printf("AZURE_ATELET_CLIENT_ID=%s\n", clientID)

	if err := createAteletFederatedCredentialIdempotent(ctx, federatedCredsClient, env, issuerURL); err != nil {
		return "", err
	}

	return principalID, nil
}

func aksOIDCIssuerURL(ctx context.Context, client *armcontainerservice.ManagedClustersClient, env *AteletWorkloadIdentityEnvironment) (string, error) {
	slog.Info("Getting AKS OIDC issuer URL", slog.String("resourceGroup", env.ResourceGroup), slog.String("cluster", env.ClusterName))
	resp, err := client.Get(ctx, env.ResourceGroup, env.ClusterName, nil)
	if err != nil {
		return "", fmt.Errorf("get AKS cluster %s: %w", env.ClusterName, err)
	}
	if resp.ManagedCluster.Properties == nil || resp.ManagedCluster.Properties.OidcIssuerProfile == nil || resp.ManagedCluster.Properties.OidcIssuerProfile.IssuerURL == nil || *resp.ManagedCluster.Properties.OidcIssuerProfile.IssuerURL == "" {
		return "", fmt.Errorf("AKS cluster %s has no OIDC issuer URL; run --create-cluster with OIDC issuer enabled first", env.ClusterName)
	}
	return *resp.ManagedCluster.Properties.OidcIssuerProfile.IssuerURL, nil
}

func createAteletIdentityIdempotent(ctx context.Context, client *armmsi.UserAssignedIdentitiesClient, env *AteletWorkloadIdentityEnvironment) (armmsi.Identity, error) {
	slog.Info("Checking if atelet managed identity exists", slog.String("resourceGroup", env.IdentityResourceGroup), slog.String("identity", env.IdentityName))
	resp, err := client.Get(ctx, env.IdentityResourceGroup, env.IdentityName, nil)
	if err != nil {
		if !isNotFound(err) {
			return armmsi.Identity{}, fmt.Errorf("get managed identity %s: %w", env.IdentityName, err)
		}

		slog.Info("Atelet managed identity does not exist. Creating...", slog.String("resourceGroup", env.IdentityResourceGroup), slog.String("identity", env.IdentityName), slog.String("location", env.Location))
		createResp, err := client.CreateOrUpdate(ctx, env.IdentityResourceGroup, env.IdentityName, armmsi.Identity{
			Location: to.Ptr(env.Location),
		}, nil)
		if err != nil {
			return armmsi.Identity{}, fmt.Errorf("create managed identity %s: %w", env.IdentityName, err)
		}
		return createResp.Identity, nil
	}

	if resp.Identity.Location != nil && !strings.EqualFold(*resp.Identity.Location, env.Location) {
		return armmsi.Identity{}, fmt.Errorf("managed identity %s is in location %s, but expected %s", env.IdentityName, *resp.Identity.Location, env.Location)
	}

	slog.Info("Atelet managed identity already exists", slog.String("resourceGroup", env.IdentityResourceGroup), slog.String("identity", env.IdentityName))
	return resp.Identity, nil
}

func userAssignedIdentityPrincipalID(identity armmsi.Identity, identityName string) (string, error) {
	if identity.Properties == nil || identity.Properties.PrincipalID == nil || *identity.Properties.PrincipalID == "" {
		return "", fmt.Errorf("managed identity %s has no principal ID in Azure response", identityName)
	}
	return *identity.Properties.PrincipalID, nil
}

func userAssignedIdentityClientID(identity armmsi.Identity, identityName string) (string, error) {
	if identity.Properties == nil || identity.Properties.ClientID == nil || *identity.Properties.ClientID == "" {
		return "", fmt.Errorf("managed identity %s has no client ID in Azure response", identityName)
	}
	return *identity.Properties.ClientID, nil
}

func createAteletFederatedCredentialIdempotent(ctx context.Context, client *armmsi.FederatedIdentityCredentialsClient, env *AteletWorkloadIdentityEnvironment, issuerURL string) error {
	subject := fmt.Sprintf("system:serviceaccount:%s:%s", env.KSANamespace, env.KSAName)

	slog.Info("Checking if atelet federated identity credential exists", slog.String("identity", env.IdentityName), slog.String("credential", env.FederatedCredName))
	resp, err := client.Get(ctx, env.IdentityResourceGroup, env.IdentityName, env.FederatedCredName, nil)
	if err != nil {
		if !isNotFound(err) {
			return fmt.Errorf("get federated identity credential %s: %w", env.FederatedCredName, err)
		}

		slog.Info("Atelet federated identity credential does not exist. Creating...", slog.String("identity", env.IdentityName), slog.String("credential", env.FederatedCredName), slog.String("subject", subject))
		_, err = client.CreateOrUpdate(ctx, env.IdentityResourceGroup, env.IdentityName, env.FederatedCredName, armmsi.FederatedIdentityCredential{
			Properties: &armmsi.FederatedIdentityCredentialProperties{
				Issuer:    to.Ptr(issuerURL),
				Subject:   to.Ptr(subject),
				Audiences: []*string{to.Ptr(azureADTokenExchangeAudience)},
			},
		}, nil)
		if err != nil {
			return fmt.Errorf("create federated identity credential %s: %w", env.FederatedCredName, err)
		}
		return nil
	}

	props := resp.FederatedIdentityCredential.Properties
	if props == nil {
		return fmt.Errorf("federated identity credential %s has no properties in Azure response", env.FederatedCredName)
	}
	if props.Issuer == nil || *props.Issuer != issuerURL {
		return fmt.Errorf("federated identity credential %s issuer mismatch; current=%q expected=%q", env.FederatedCredName, stringPtrValue(props.Issuer), issuerURL)
	}
	if props.Subject == nil || *props.Subject != subject {
		return fmt.Errorf("federated identity credential %s subject mismatch; current=%q expected=%q", env.FederatedCredName, stringPtrValue(props.Subject), subject)
	}
	if !containsStringPtr(props.Audiences, azureADTokenExchangeAudience) {
		return fmt.Errorf("federated identity credential %s audience missing %q", env.FederatedCredName, azureADTokenExchangeAudience)
	}

	slog.Info("Atelet federated identity credential already exists", slog.String("identity", env.IdentityName), slog.String("credential", env.FederatedCredName))
	return nil
}

func createSnapshotStorageRoleAssignmentIdempotent(ctx context.Context, client *armauthorization.RoleAssignmentsClient, subscriptionID string, env *AteletPermissionsEnvironment, principalID string) error {
	scope := snapshotContainerScope(subscriptionID, env)
	roleDefinitionID := storageBlobDataContributorRoleDefinitionID(subscriptionID)
	return createRoleAssignmentIdempotent(ctx, client, scope, roleDefinitionID, principalID, "atelet snapshot storage")
}

func createAteletAcrPullRoleAssignmentIdempotent(ctx context.Context, client *armauthorization.RoleAssignmentsClient, subscriptionID string, env *AteletPermissionsEnvironment, principalID string) error {
	scope := acrScope(subscriptionID, env.ContainerRegistryResourceGroup, env.ContainerRegistryName)
	roleDefinitionID := acrPullRoleDefinitionID(subscriptionID)
	return createRoleAssignmentIdempotent(ctx, client, scope, roleDefinitionID, principalID, "atelet AcrPull")
}

func createRoleAssignmentIdempotent(ctx context.Context, client *armauthorization.RoleAssignmentsClient, scope, roleDefinitionID, principalID, label string) error {
	assignmentName := deterministicRoleAssignmentName(scope, roleDefinitionID, principalID)

	slog.Info("Checking if role assignment exists", slog.String("label", label), slog.String("scope", scope), slog.String("roleAssignment", assignmentName))
	resp, err := client.Get(ctx, scope, assignmentName, nil)
	if err != nil {
		if !isNotFound(err) {
			return fmt.Errorf("get role assignment %s: %w", assignmentName, err)
		}

		slog.Info("Role assignment does not exist. Creating...", slog.String("label", label), slog.String("scope", scope), slog.String("principalID", principalID))
		_, err = client.Create(ctx, scope, assignmentName, armauthorization.RoleAssignmentCreateParameters{
			Properties: &armauthorization.RoleAssignmentProperties{
				PrincipalID:      to.Ptr(principalID),
				RoleDefinitionID: to.Ptr(roleDefinitionID),
			},
		}, nil)
		if err != nil {
			return fmt.Errorf("create role assignment %s: %w", assignmentName, err)
		}
		return nil
	}

	props := resp.RoleAssignment.Properties
	if props == nil {
		return fmt.Errorf("role assignment %s has no properties in Azure response", assignmentName)
	}
	if props.PrincipalID == nil || !strings.EqualFold(*props.PrincipalID, principalID) {
		return fmt.Errorf("role assignment %s principal mismatch; current=%q expected=%q", assignmentName, stringPtrValue(props.PrincipalID), principalID)
	}
	if props.RoleDefinitionID == nil || !strings.EqualFold(*props.RoleDefinitionID, roleDefinitionID) {
		return fmt.Errorf("role assignment %s role definition mismatch; current=%q expected=%q", assignmentName, stringPtrValue(props.RoleDefinitionID), roleDefinitionID)
	}

	slog.Info("Role assignment already exists", slog.String("label", label), slog.String("scope", scope), slog.String("roleAssignment", assignmentName))
	return nil
}

func snapshotContainerScope(subscriptionID string, env *AteletPermissionsEnvironment) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Storage/storageAccounts/%s/blobServices/default/containers/%s", subscriptionID, env.ResourceGroup, env.StorageAccountName, env.StorageContainerName)
}

func storageBlobDataContributorRoleDefinitionID(subscriptionID string) string {
	return fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subscriptionID, storageBlobDataContributorRoleID)
}

func deterministicRoleAssignmentName(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "|"))).String()
}

func containsStringPtr(values []*string, want string) bool {
	for _, value := range values {
		if value != nil && *value == want {
			return true
		}
	}
	return false
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
