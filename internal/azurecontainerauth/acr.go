// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package azurecontainerauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/google/go-containerregistry/pkg/authn"
)

const managementScope = "https://management.azure.com/.default"

type ACRAuthenticator struct {
	registryHost string
	cred         azcore.TokenCredential
	now          func() time.Time

	mu           sync.Mutex
	refreshToken string
	expiresOn    time.Time
}

func NewACRAuthenticator(registryHost string, cred azcore.TokenCredential) (*ACRAuthenticator, error) {
	registryHost = normalizeRegistryHost(registryHost)
	if registryHost == "" {
		return nil, fmt.Errorf("ACR registry host is required")
	}
	if cred == nil {
		return nil, fmt.Errorf("Azure credential is required")
	}
	return &ACRAuthenticator{
		registryHost: registryHost,
		cred:         cred,
		now:          time.Now,
	}, nil
}

func (a *ACRAuthenticator) Authorization() (*authn.AuthConfig, error) {
	return a.AuthorizationContext(context.Background())
}

func (a *ACRAuthenticator) AuthorizationContext(ctx context.Context) (*authn.AuthConfig, error) {
	refreshToken, err := a.getRefreshToken(ctx)
	if err != nil {
		return nil, err
	}
	return &authn.AuthConfig{IdentityToken: refreshToken}, nil
}

func (a *ACRAuthenticator) getRefreshToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.refreshToken != "" && a.now().Before(a.expiresOn.Add(-5*time.Minute)) {
		return a.refreshToken, nil
	}

	tok, err := a.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{managementScope}})
	if err != nil {
		return "", fmt.Errorf("get Azure token for ACR: %w", err)
	}

	refreshToken, err := exchangeAADTokenForACRRefreshToken(ctx, a.registryHost, tok.Token)
	if err != nil {
		return "", err
	}
	a.refreshToken = refreshToken
	a.expiresOn = tok.ExpiresOn
	return refreshToken, nil
}

func exchangeAADTokenForACRRefreshToken(ctx context.Context, registryHost, aadToken string) (string, error) {
	endpoint := "https://" + registryHost + "/oauth2/exchange"
	values := url.Values{}
	values.Set("grant_type", "access_token")
	values.Set("service", registryHost)
	values.Set("access_token", aadToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return "", fmt.Errorf("create ACR token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange Azure token for ACR refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("exchange Azure token for ACR refresh token: unexpected status %s", resp.Status)
	}

	var out struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode ACR token exchange response: %w", err)
	}
	if out.RefreshToken == "" {
		return "", fmt.Errorf("ACR token exchange response did not include refresh_token")
	}
	return out.RefreshToken, nil
}

func normalizeRegistryHost(registryHost string) string {
	registryHost = strings.TrimSpace(registryHost)
	registryHost = strings.TrimPrefix(registryHost, "https://")
	registryHost = strings.TrimPrefix(registryHost, "http://")
	registryHost = strings.TrimSuffix(registryHost, "/")
	return registryHost
}
