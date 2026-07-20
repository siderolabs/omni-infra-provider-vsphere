// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider_test

import (
	"testing"

	"github.com/siderolabs/omni-infra-provider-vsphere/internal/pkg/provider"
)

func TestDataValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    provider.Data
		wantErr bool
	}{
		{"template only", provider.Data{Template: "tmpl"}, false},
		{"content library only", provider.Data{ContentLibrary: "lib", LibraryItem: "item"}, false},
		{"both sources set", provider.Data{Template: "tmpl", ContentLibrary: "lib", LibraryItem: "item"}, true},
		{"no source set", provider.Data{}, true},
		{"library missing item", provider.Data{ContentLibrary: "lib"}, true},
		{"library missing library name", provider.Data{LibraryItem: "item"}, true},
		{"template plus stray library name", provider.Data{Template: "tmpl", ContentLibrary: "lib"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.data.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
