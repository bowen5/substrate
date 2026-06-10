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

package ategcs

import (
	"context"
	"io"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

type azureBlobClient struct {
	client *azblob.Client
}

func NewAzureBlobClient(client *azblob.Client) ObjectStorage {
	return &azureBlobClient{client: client}
}

func (a *azureBlobClient) GetObject(ctx context.Context, container, object string) (io.ReadCloser, error) {
	resp, err := a.client.DownloadStream(ctx, container, object, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (a *azureBlobClient) PutObject(ctx context.Context, container, object string, reader io.Reader) error {
	_, err := a.client.UploadStream(ctx, container, object, reader, nil)
	return err
}
