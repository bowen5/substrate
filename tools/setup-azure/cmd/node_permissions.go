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
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
)

const acrPullRoleID = "7f951dda-4ed3-4680-a7ca-43fe172d538d"

func grantAksNodePermissions(ctx context.Context, env *Environment) error {
	nodeEnv, err := requireAksNodePermissionsEnv()
	if err != nil {
		return err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("create Azure default credential: %w", err)
	}

	clustersClient, err := armcontainerservice.NewManagedClustersClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure managed clusters client: %w", err)
	}

	roleAssignmentsClient, err := armauthorization.NewRoleAssignmentsClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure role assignments client: %w", err)
	}

	principalID, err := aksKubeletIdentityPrincipalID(ctx, clustersClient, nodeEnv)
	if err != nil {
		return err
	}

	return createAcrPullRoleAssignmentIdempotent(ctx, roleAssignmentsClient, env.SubscriptionID, nodeEnv, principalID)
}

func aksKubeletIdentityPrincipalID(ctx context.Context, client *armcontainerservice.ManagedClustersClient, env *AksNodePermissionsEnvironment) (string, error) {
	slog.Info("Getting AKS kubelet identity", slog.String("resourceGroup", env.ResourceGroup), slog.String("cluster", env.ClusterName))
	resp, err := client.Get(ctx, env.ResourceGroup, env.ClusterName, nil)
	if err != nil {
		return "", fmt.Errorf("get AKS cluster %s: %w", env.ClusterName, err)
	}
	if resp.ManagedCluster.Properties == nil || resp.ManagedCluster.Properties.IdentityProfile == nil {
		return "", fmt.Errorf("AKS cluster %s has no identity profile in Azure response", env.ClusterName)
	}

	kubeletIdentity := resp.ManagedCluster.Properties.IdentityProfile["kubeletidentity"]
	if kubeletIdentity == nil || kubeletIdentity.ObjectID == nil || *kubeletIdentity.ObjectID == "" {
		return "", fmt.Errorf("AKS cluster %s has no kubeletidentity object ID in identity profile", env.ClusterName)
	}
	return *kubeletIdentity.ObjectID, nil
}

func createAcrPullRoleAssignmentIdempotent(ctx context.Context, client *armauthorization.RoleAssignmentsClient, subscriptionID string, env *AksNodePermissionsEnvironment, principalID string) error {
	scope := acrScope(subscriptionID, env.ContainerRegistryResourceGroup, env.ContainerRegistryName)
	roleDefinitionID := acrPullRoleDefinitionID(subscriptionID)
	assignmentName := deterministicRoleAssignmentName(scope, roleDefinitionID, principalID)

	slog.Info("Checking if AKS kubelet AcrPull role assignment exists", slog.String("scope", scope), slog.String("roleAssignment", assignmentName))
	resp, err := client.Get(ctx, scope, assignmentName, nil)
	if err != nil {
		if !isNotFound(err) {
			return fmt.Errorf("get role assignment %s: %w", assignmentName, err)
		}

		slog.Info("AKS kubelet AcrPull role assignment does not exist. Creating...", slog.String("scope", scope), slog.String("principalID", principalID))
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

	slog.Info("AKS kubelet AcrPull role assignment already exists", slog.String("scope", scope), slog.String("roleAssignment", assignmentName))
	return nil
}

func acrScope(subscriptionID, resourceGroup, registryName string) string {
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerRegistry/registries/%s", subscriptionID, resourceGroup, registryName)
}

func acrPullRoleDefinitionID(subscriptionID string) string {
	return fmt.Sprintf("/subscriptions/%s/providers/Microsoft.Authorization/roleDefinitions/%s", subscriptionID, acrPullRoleID)
}
