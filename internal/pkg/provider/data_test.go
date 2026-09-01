// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider_test

import (
	"testing"

	"github.com/vmware/govmomi/vim25/types"

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
		{
			"pci device by label",
			provider.Data{Template: "tmpl", PCIDevices: []provider.PCIDevice{{Label: "gpu"}}},
			false,
		},
		{
			"pci device by address",
			provider.Data{Template: "tmpl", PCIDevices: []provider.PCIDevice{{Address: "0000:06:00.0"}}},
			false,
		},
		{
			"pci device with both label and address",
			provider.Data{Template: "tmpl", PCIDevices: []provider.PCIDevice{{Label: "gpu", Address: "0000:06:00.0"}}},
			true,
		},
		{
			"pci device with neither label nor address",
			provider.Data{Template: "tmpl", PCIDevices: []provider.PCIDevice{{}}},
			true,
		},
		{
			"second pci device invalid",
			provider.Data{Template: "tmpl", PCIDevices: []provider.PCIDevice{{Label: "gpu"}, {}}},
			true,
		},
		{"firmware efi", provider.Data{Template: "tmpl", Firmware: "efi"}, false},
		{"firmware bios", provider.Data{Template: "tmpl", Firmware: "bios"}, false},
		{"firmware uefi alias", provider.Data{Template: "tmpl", Firmware: "UEFI"}, false},
		{"firmware unknown", provider.Data{Template: "tmpl", Firmware: "openfirmware"}, true},
		{
			"extra config",
			provider.Data{Template: "tmpl", ExtraConfig: []provider.ExtraConfigItem{
				{Key: "pciPassthru.use64bitMMIO", Value: "TRUE"},
			}},
			false,
		},
		{
			"extra config with empty key",
			provider.Data{Template: "tmpl", ExtraConfig: []provider.ExtraConfigItem{{Value: "TRUE"}}},
			true,
		},
		{
			"extra config with empty value",
			provider.Data{Template: "tmpl", ExtraConfig: []provider.ExtraConfigItem{{Key: "svga.present"}}},
			true,
		},
		{
			"extra config with duplicate key",
			provider.Data{Template: "tmpl", ExtraConfig: []provider.ExtraConfigItem{
				{Key: "svga.present", Value: "TRUE"},
				{Key: "svga.present", Value: "FALSE"},
			}},
			true,
		},
		{
			"extra config overriding talos config",
			provider.Data{Template: "tmpl", ExtraConfig: []provider.ExtraConfigItem{
				{Key: "guestinfo.talos.config", Value: "evil"},
			}},
			true,
		},
		{
			"extra config overriding disk uuid",
			provider.Data{Template: "tmpl", ExtraConfig: []provider.ExtraConfigItem{
				{Key: "disk.enableUUID", Value: "FALSE"},
			}},
			true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.data.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestFirmwareType(t *testing.T) {
	for _, tc := range []struct {
		name     string
		firmware string
		want     types.GuestOsDescriptorFirmwareType
		wantOK   bool
	}{
		{"unset", "", "", true},
		{"bios", "bios", types.GuestOsDescriptorFirmwareTypeBios, true},
		{"efi", "efi", types.GuestOsDescriptorFirmwareTypeEfi, true},
		{"uefi is an alias for efi", "uefi", types.GuestOsDescriptorFirmwareTypeEfi, true},
		{"mixed case", "EFI", types.GuestOsDescriptorFirmwareTypeEfi, true},
		{"unknown", "nonsense", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := provider.FirmwareType(tc.firmware)

			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("FirmwareType(%q) = %q/%v, want %q/%v", tc.firmware, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestApplyFirmware(t *testing.T) {
	for _, tc := range []struct {
		name     string
		firmware string
		want     string
	}{
		// An unset firmware must leave the spec alone, so that machine classes written
		// before this setting existed keep inheriting the template's firmware.
		{"unset leaves the spec untouched", "", ""},
		{"bios", "bios", "bios"},
		{"efi", "efi", "efi"},
		{"uefi is normalized to efi", "uefi", "efi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var spec types.VirtualMachineConfigSpec

			provider.ApplyFirmware(&spec, provider.Data{Firmware: tc.firmware})

			if spec.Firmware != tc.want {
				t.Fatalf("spec.Firmware = %q, want %q", spec.Firmware, tc.want)
			}
		})
	}
}

func TestExtraConfigOptions(t *testing.T) {
	for _, tc := range []struct {
		want [][2]string
		name string
		data provider.Data
	}{
		{
			name: "provider keys only",
			data: provider.Data{},
			want: [][2]string{
				{"disk.enableUUID", "TRUE"},
				{"guestinfo.talos.config", "cfg"},
			},
		},
		{
			name: "user keys follow provider keys in the configured order",
			data: provider.Data{ExtraConfig: []provider.ExtraConfigItem{
				{Key: "svga.present", Value: "FALSE"},
				{Key: "pciPassthru.use64bitMMIO", Value: "TRUE"},
				{Key: "pciPassthru.64bitMMIOSizeGB", Value: "128"},
			}},
			want: [][2]string{
				{"disk.enableUUID", "TRUE"},
				{"guestinfo.talos.config", "cfg"},
				{"svga.present", "FALSE"},
				{"pciPassthru.use64bitMMIO", "TRUE"},
				{"pciPassthru.64bitMMIOSizeGB", "128"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := provider.ExtraConfigOptions(tc.data, "cfg")

			if len(options) != len(tc.want) {
				t.Fatalf("got %d options, want %d", len(options), len(tc.want))
			}

			for i, option := range options {
				value, ok := option.(*types.OptionValue)
				if !ok {
					t.Fatalf("option %d is %T, want *types.OptionValue", i, option)
				}

				if value.Key != tc.want[i][0] || value.Value != tc.want[i][1] {
					t.Errorf("option %d = %q/%v, want %q/%q", i, value.Key, value.Value, tc.want[i][0], tc.want[i][1])
				}
			}
		})
	}
}
