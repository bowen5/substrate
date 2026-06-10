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

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
)

var requiredResourceProviders = []string{
	"Microsoft.ContainerService",  // AKS
	"Microsoft.Cache",             // Azure Cache for Redis
	"Microsoft.Storage",           // Storage account / Blob
	"Microsoft.ContainerRegistry", // ACR
	"Microsoft.ManagedIdentity",   // Workload identity / user-assigned identities
	"Microsoft.Network",           // AKS networking / load balancers
}

func registerResourceProviders(ctx context.Context, env *Environment) error {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("create Azure default credential: %w", err)
	}

	client, err := armresources.NewProvidersClient(env.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("create Azure providers client: %w", err)
	}

	for _, namespace := range requiredResourceProviders {
		if err := registerResourceProvider(ctx, client, namespace); err != nil {
			return err
		}
	}

	return nil
}

func registerResourceProvider(ctx context.Context, client *armresources.ProvidersClient, namespace string) error {
	state, err := resourceProviderRegistrationState(ctx, client, namespace)
	if err != nil {
		return fmt.Errorf("get resource provider %s: %w", namespace, err)
	}
	if strings.EqualFold(state, "Registered") {
		slog.Info("Resource provider already registered", slog.String("namespace", namespace))
		return nil
	}

	slog.Info("Registering resource provider", slog.String("namespace", namespace), slog.String("currentState", state))
	if _, err := client.Register(ctx, namespace, nil); err != nil {
		return fmt.Errorf("register resource provider %s: %w", namespace, err)
	}

	return waitForResourceProviderRegistration(ctx, client, namespace)
}

func waitForResourceProviderRegistration(ctx context.Context, client *armresources.ProvidersClient, namespace string) error {
	pollCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		state, err := resourceProviderRegistrationState(pollCtx, client, namespace)
		if err != nil {
			return fmt.Errorf("poll resource provider %s: %w", namespace, err)
		}
		if strings.EqualFold(state, "Registered") {
			slog.Info("Resource provider registered", slog.String("namespace", namespace))
			return nil
		}

		slog.Info("Waiting for resource provider registration", slog.String("namespace", namespace), slog.String("state", state))

		select {
		case <-pollCtx.Done():
			return fmt.Errorf("timed out waiting for resource provider %s to register: %w", namespace, pollCtx.Err())
		case <-ticker.C:
		}
	}
}

func resourceProviderRegistrationState(ctx context.Context, client *armresources.ProvidersClient, namespace string) (string, error) {
	resp, err := client.Get(ctx, namespace, nil)
	if err != nil {
		return "", err
	}
	if resp.Provider.RegistrationState == nil {
		return "", nil
	}
	return *resp.Provider.RegistrationState, nil
}
