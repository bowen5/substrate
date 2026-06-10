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
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Environment struct {
	SubscriptionID string
}

type ClusterEnvironment struct {
	ResourceGroup     string
	Location          string
	ClusterName       string
	KubernetesVersion string
	NodePoolName      string
	NodeCount         int32
	NodeVMSize        string
	DNSPrefix         string
	VnetSubnetID      string
}

type SnapshotStorageEnvironment struct {
	ResourceGroup string
	Location      string
	AccountName   string
	ContainerName string
	SKUName       string
}

type StorageRoleAssignmentsEnvironment struct {
	ResourceGroup         string
	Location              string
	ClusterName           string
	StorageAccountName    string
	StorageContainerName  string
	IdentityResourceGroup string
	IdentityName          string
	KSANamespace          string
	KSAName               string
	FederatedCredName     string
}

var (
	storageAccountNamePattern = regexp.MustCompile(`^[a-z0-9]{3,24}$`)
	blobContainerNamePattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,61}[a-z0-9])?$`)
)

func loadEnv() (*Environment, error) {
	requiredEnvVars := []string{
		"AZURE_SUBSCRIPTION_ID",
	}

	missing := []string{}
	for _, key := range requiredEnvVars {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return &Environment{
		SubscriptionID: os.Getenv("AZURE_SUBSCRIPTION_ID"),
	}, nil
}

func requireClusterEnv() (*ClusterEnvironment, error) {
	requiredEnvVars := []string{
		"AZURE_RESOURCE_GROUP",
		"AZURE_LOCATION",
		"AKS_CLUSTER_NAME",
	}

	missing := []string{}
	for _, key := range requiredEnvVars {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables for cluster setup: %s", strings.Join(missing, ", "))
	}

	nodeCount, err := parseEnvInt32("AKS_NODE_COUNT", 2)
	if err != nil {
		return nil, err
	}

	clusterName := os.Getenv("AKS_CLUSTER_NAME")
	dnsPrefix := envOrDefault("AKS_DNS_PREFIX", clusterName)

	return &ClusterEnvironment{
		ResourceGroup:     os.Getenv("AZURE_RESOURCE_GROUP"),
		Location:          os.Getenv("AZURE_LOCATION"),
		ClusterName:       clusterName,
		KubernetesVersion: envOrDefault("AKS_KUBERNETES_VERSION", "1.34.8"),
		NodePoolName:      envOrDefault("AKS_NODE_POOL_NAME", "substrate"),
		NodeCount:         nodeCount,
		NodeVMSize:        envOrDefault("AKS_NODE_VM_SIZE", "Standard_D4ads_v5"),
		DNSPrefix:         dnsPrefix,
		VnetSubnetID:      os.Getenv("AKS_VNET_SUBNET_ID"),
	}, nil
}

func requireSnapshotStorageEnv() (*SnapshotStorageEnvironment, error) {
	requiredEnvVars := []string{
		"AZURE_RESOURCE_GROUP",
		"AZURE_LOCATION",
		"AZURE_STORAGE_ACCOUNT_NAME",
	}

	missing := []string{}
	for _, key := range requiredEnvVars {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables for snapshot storage setup: %s", strings.Join(missing, ", "))
	}

	accountName := os.Getenv("AZURE_STORAGE_ACCOUNT_NAME")
	if !storageAccountNamePattern.MatchString(accountName) {
		return nil, fmt.Errorf("AZURE_STORAGE_ACCOUNT_NAME must be 3-24 characters using only lowercase letters and numbers")
	}

	containerName := envOrDefault("AZURE_STORAGE_CONTAINER_NAME", "ate-snapshots")
	if !blobContainerNamePattern.MatchString(containerName) || strings.Contains(containerName, "--") {
		return nil, fmt.Errorf("AZURE_STORAGE_CONTAINER_NAME must be 3-63 characters using lowercase letters, numbers, and single dashes; dashes must be between letters or numbers")
	}

	return &SnapshotStorageEnvironment{
		ResourceGroup: os.Getenv("AZURE_RESOURCE_GROUP"),
		Location:      os.Getenv("AZURE_LOCATION"),
		AccountName:   accountName,
		ContainerName: containerName,
		SKUName:       envOrDefault("AZURE_STORAGE_ACCOUNT_SKU", "Standard_LRS"),
	}, nil
}

func requireStorageRoleAssignmentsEnv() (*StorageRoleAssignmentsEnvironment, error) {
	requiredEnvVars := []string{
		"AZURE_RESOURCE_GROUP",
		"AZURE_LOCATION",
		"AKS_CLUSTER_NAME",
		"AZURE_STORAGE_ACCOUNT_NAME",
	}

	missing := []string{}
	for _, key := range requiredEnvVars {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables for storage role assignment setup: %s", strings.Join(missing, ", "))
	}

	accountName := os.Getenv("AZURE_STORAGE_ACCOUNT_NAME")
	if !storageAccountNamePattern.MatchString(accountName) {
		return nil, fmt.Errorf("AZURE_STORAGE_ACCOUNT_NAME must be 3-24 characters using only lowercase letters and numbers")
	}

	containerName := envOrDefault("AZURE_STORAGE_CONTAINER_NAME", "ate-snapshots")
	if !blobContainerNamePattern.MatchString(containerName) || strings.Contains(containerName, "--") {
		return nil, fmt.Errorf("AZURE_STORAGE_CONTAINER_NAME must be 3-63 characters using lowercase letters, numbers, and single dashes; dashes must be between letters or numbers")
	}

	ksaNamespace := envOrDefault("AZURE_ATELET_KSA_NAMESPACE", "ate-system")
	ksaName := envOrDefault("AZURE_ATELET_KSA_NAME", "atelet")

	return &StorageRoleAssignmentsEnvironment{
		ResourceGroup:         os.Getenv("AZURE_RESOURCE_GROUP"),
		Location:              os.Getenv("AZURE_LOCATION"),
		ClusterName:           os.Getenv("AKS_CLUSTER_NAME"),
		StorageAccountName:    accountName,
		StorageContainerName:  containerName,
		IdentityResourceGroup: envOrDefault("AZURE_ATELET_IDENTITY_RESOURCE_GROUP", os.Getenv("AZURE_RESOURCE_GROUP")),
		IdentityName:          envOrDefault("AZURE_ATELET_IDENTITY_NAME", "atelet"),
		KSANamespace:          ksaNamespace,
		KSAName:               ksaName,
		FederatedCredName:     envOrDefault("AZURE_ATELET_FEDERATED_CREDENTIAL_NAME", ksaNamespace+"-"+ksaName),
	}, nil
}

func envOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func parseEnvInt32(key string, defaultValue int32) (int32, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid int32: %w", key, err)
	}
	if parsed < 1 {
		return 0, fmt.Errorf("%s must be at least 1", key)
	}
	return int32(parsed), nil
}
