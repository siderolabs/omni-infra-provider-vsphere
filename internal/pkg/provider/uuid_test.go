// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import "testing"

func TestConvertVSphereUUIDToTalosFormat(t *testing.T) {
	tests := []struct {
		name        string
		vsphereUUID string
		want        string
		wantErr     bool
	}{
		{
			name:        "real example from logs",
			vsphereUUID: "422413c3-57c8-96d1-c481-c58dbb837d2d",
			want:        "c3132442-c857-d196-c481-c58dbb837d2d",
			wantErr:     false,
		},
		{
			name:        "another real example",
			vsphereUUID: "42248383-124d-4f71-06fa-6b6d74a978c1",
			want:        "83832442-4d12-714f-06fa-6b6d74a978c1",
			wantErr:     false,
		},
		{
			name:        "invalid length",
			vsphereUUID: "422413c3-57c8",
			want:        "",
			wantErr:     true,
		},
		{
			name:        "invalid hex",
			vsphereUUID: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
			want:        "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertVSphereUUIDToTalosFormat(tt.vsphereUUID)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertVSphereUUIDToTalosFormat() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("convertVSphereUUIDToTalosFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}
