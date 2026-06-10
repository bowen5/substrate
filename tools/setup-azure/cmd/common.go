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
		KubernetesVersion: os.Getenv("AKS_KUBERNETES_VERSION"),
		NodePoolName:      envOrDefault("AKS_NODE_POOL_NAME", "substrate"),
		NodeCount:         nodeCount,
		NodeVMSize:        envOrDefault("AKS_NODE_VM_SIZE", "Standard_D4ads_v5"),
		DNSPrefix:         dnsPrefix,
		VnetSubnetID:      os.Getenv("AKS_VNET_SUBNET_ID"),
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
