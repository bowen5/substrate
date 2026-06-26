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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

func createClusterIdempotent(ctx context.Context, env *Environment) error {
	clusterEnv, err := requireClusterEnv()
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

	managedClustersClient, err := armcontainerservice.NewManagedClustersClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure managed clusters client: %w", err)
	}

	if err := createResourceGroup(ctx, resourceGroupsClient, clusterEnv.ResourceGroup, clusterEnv.Location); err != nil {
		return err
	}

	slog.Info("Checking if AKS cluster exists", slog.String("resourceGroup", clusterEnv.ResourceGroup), slog.String("cluster", clusterEnv.ClusterName))
	clusterResp, err := managedClustersClient.Get(ctx, clusterEnv.ResourceGroup, clusterEnv.ClusterName, nil)
	if err != nil {
		if !isNotFound(err) {
			return fmt.Errorf("get AKS cluster: %w", err)
		}
		return createClusterInternal(ctx, managedClustersClient, clusterEnv)
	}

	return validateExistingCluster(clusterResp.ManagedCluster, clusterEnv)
}

func createResourceGroup(ctx context.Context, client *armresources.ResourceGroupsClient, resourceGroup, location string) error {
	slog.Info("Creating/updating Azure resource group", slog.String("resourceGroup", resourceGroup), slog.String("location", location))
	_, err := client.CreateOrUpdate(ctx, resourceGroup, armresources.ResourceGroup{
		Location: to.Ptr(location),
	}, nil)
	if err != nil {
		return fmt.Errorf("create/update resource group %s: %w", resourceGroup, err)
	}
	return nil
}

func createClusterInternal(ctx context.Context, client *armcontainerservice.ManagedClustersClient, env *ClusterEnvironment) error {
	slog.Info("AKS cluster does not exist. Creating...", slog.String("resourceGroup", env.ResourceGroup), slog.String("cluster", env.ClusterName))

	cluster := armcontainerservice.ManagedCluster{
		Location: to.Ptr(env.Location),
		Identity: &armcontainerservice.ManagedClusterIdentity{
			Type: to.Ptr(armcontainerservice.ResourceIdentityTypeSystemAssigned),
		},
		SKU: &armcontainerservice.ManagedClusterSKU{
			Name: to.Ptr(armcontainerservice.ManagedClusterSKUNameBase),
			Tier: to.Ptr(armcontainerservice.ManagedClusterSKUTierStandard),
		},
		Properties: &armcontainerservice.ManagedClusterProperties{
			DNSPrefix:         to.Ptr(env.DNSPrefix),
			EnableRBAC:        to.Ptr(true),
			KubernetesVersion: optionalStringPtr(env.KubernetesVersion),
			AgentPoolProfiles: []*armcontainerservice.ManagedClusterAgentPoolProfile{
				{
					Name:                to.Ptr(env.NodePoolName),
					Count:               to.Ptr(env.NodeCount),
					VMSize:              to.Ptr(env.NodeVMSize),
					Type:                to.Ptr(armcontainerservice.AgentPoolTypeVirtualMachineScaleSets),
					Mode:                to.Ptr(armcontainerservice.AgentPoolModeSystem),
					OSType:              to.Ptr(armcontainerservice.OSTypeLinux),
					OrchestratorVersion: optionalStringPtr(env.KubernetesVersion),
					VnetSubnetID:        optionalStringPtr(env.VnetSubnetID),
				},
			},
			NetworkProfile: &armcontainerservice.NetworkProfile{
				NetworkPlugin:     to.Ptr(armcontainerservice.NetworkPluginAzure),
				NetworkPluginMode: to.Ptr(armcontainerservice.NetworkPluginModeOverlay),
				LoadBalancerSKU:   to.Ptr(armcontainerservice.LoadBalancerSKUStandard),
				OutboundType:      to.Ptr(armcontainerservice.OutboundTypeLoadBalancer),
			},
			OidcIssuerProfile: &armcontainerservice.ManagedClusterOIDCIssuerProfile{
				Enabled: to.Ptr(true),
			},
			SecurityProfile: &armcontainerservice.ManagedClusterSecurityProfile{
				WorkloadIdentity: &armcontainerservice.ManagedClusterSecurityProfileWorkloadIdentity{
					Enabled: to.Ptr(true),
				},
			},
		},
	}

	createCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()

	poller, err := client.BeginCreateOrUpdate(createCtx, env.ResourceGroup, env.ClusterName, cluster, nil)
	if err != nil {
		return fmt.Errorf("begin create AKS cluster: %w", err)
	}

	_, err = poller.PollUntilDone(createCtx, &runtime.PollUntilDoneOptions{Frequency: 30 * time.Second})
	if err != nil {
		return fmt.Errorf("create AKS cluster: %w", err)
	}

	slog.Info("AKS cluster created", slog.String("resourceGroup", env.ResourceGroup), slog.String("cluster", env.ClusterName))
	return nil
}

func validateExistingCluster(cluster armcontainerservice.ManagedCluster, env *ClusterEnvironment) error {
	slog.Info("AKS cluster exists. Checking attributes...", slog.String("cluster", env.ClusterName))

	if cluster.Location != nil && !strings.EqualFold(*cluster.Location, env.Location) {
		return fmt.Errorf("AKS cluster %s is in location %s, but expected %s", env.ClusterName, *cluster.Location, env.Location)
	}

	props := cluster.Properties
	if props == nil {
		return fmt.Errorf("AKS cluster %s has no properties in Azure response", env.ClusterName)
	}

	if props.OidcIssuerProfile == nil || props.OidcIssuerProfile.Enabled == nil || !*props.OidcIssuerProfile.Enabled {
		return fmt.Errorf("AKS cluster %s exists but OIDC issuer is not enabled; refusing to mutate existing cluster automatically", env.ClusterName)
	}
	if props.SecurityProfile == nil || props.SecurityProfile.WorkloadIdentity == nil || props.SecurityProfile.WorkloadIdentity.Enabled == nil || !*props.SecurityProfile.WorkloadIdentity.Enabled {
		return fmt.Errorf("AKS cluster %s exists but workload identity is not enabled; refusing to mutate existing cluster automatically", env.ClusterName)
	}

	if env.VnetSubnetID != "" {
		if !existingClusterHasSubnet(props.AgentPoolProfiles, env.VnetSubnetID) {
			return fmt.Errorf("AKS cluster %s exists but no agent pool uses AKS_VNET_SUBNET_ID %s; refusing destructive reconciliation", env.ClusterName, env.VnetSubnetID)
		}
	}

	slog.Info("AKS cluster attributes look compatible", slog.String("cluster", env.ClusterName))
	return nil
}

func existingClusterHasSubnet(pools []*armcontainerservice.ManagedClusterAgentPoolProfile, subnetID string) bool {
	for _, pool := range pools {
		if pool == nil || pool.VnetSubnetID == nil {
			continue
		}
		if strings.EqualFold(*pool.VnetSubnetID, subnetID) {
			return true
		}
	}
	return false
}

func optionalStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return to.Ptr(value)
}

func isNotFound(err error) bool {
	var responseErr *azcore.ResponseError
	if errors.As(err, &responseErr) {
		return responseErr.StatusCode == http.StatusNotFound
	}
	return false
}

func isAzureErrorCode(err error, code string) bool {
	var responseErr *azcore.ResponseError
	if errors.As(err, &responseErr) {
		return strings.EqualFold(responseErr.ErrorCode, code)
	}
	return false
}
