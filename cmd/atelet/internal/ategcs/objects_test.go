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

import "testing"

func TestParseObjectURL(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		wantBucket string
		wantObject string
	}{
		{name: "gcs", rawURL: "gs://bucket/prefix/checkpoint.img", wantBucket: "bucket", wantObject: "prefix/checkpoint.img"},
		{name: "s3", rawURL: "s3://bucket/prefix/checkpoint.img", wantBucket: "bucket", wantObject: "prefix/checkpoint.img"},
		{name: "azure blob", rawURL: "azblob://container/prefix/checkpoint.img", wantBucket: "container", wantObject: "prefix/checkpoint.img"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBucket, gotObject, err := parseObjectURL(tt.rawURL)
			if err != nil {
				t.Fatalf("parseObjectURL(%q) returned error: %v", tt.rawURL, err)
			}
			if gotBucket != tt.wantBucket || gotObject != tt.wantObject {
				t.Fatalf("parseObjectURL(%q) = (%q, %q); want (%q, %q)", tt.rawURL, gotBucket, gotObject, tt.wantBucket, tt.wantObject)
			}
		})
	}
}

func TestParseObjectURLRejectsInvalidURLs(t *testing.T) {
	tests := []string{
		"https://account.blob.core.windows.net/container/blob",
		"azblob:///missing-container",
		"azblob://container",
		"://bad-url",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			if _, _, err := parseObjectURL(rawURL); err == nil {
				t.Fatalf("parseObjectURL(%q) succeeded; want error", rawURL)
			}
		})
	}
}
