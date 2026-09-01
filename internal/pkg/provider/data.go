// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/vim25/types"
)

// extraConfig keys the provider owns. They carry the Talos machine config and the
// disk UUID setting Talos needs to find its install disk, so Data.ExtraConfig is
// not allowed to set them.
const (
	extraConfigKeyTalosConfig = "guestinfo.talos.config"
	extraConfigKeyDiskUUID    = "disk.enableUUID"
)

// ExtraConfigItem is a single vSphere Advanced Parameter. This is a list of
// key/value pairs rather than a map because the Omni machine class UI cannot render
// a form field for a free-form map.
type ExtraConfigItem struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

// PCIDevice selects a physical PCI device to pass through to the VM. Exactly one
// of Label or Address must be set.
type PCIDevice struct {
	// Label matches a hardware label set on the ESXi host's PCI device (Dynamic
	// DirectPath I/O, vSphere 7.0.2 and later). Preferred over Address: vCenter
	// picks any host holding a device with this label, so the machine class is not
	// tied to a single host.
	Label string `yaml:"label,omitempty"`
	// Address is a "bus:slot.function" PCI address (static DirectPath I/O). It pins
	// the VM to the host holding that device.
	Address string `yaml:"address,omitempty"`
}

// Data is the provider custom machine config.
type Data struct {
	Datacenter   string `yaml:"datacenter"`
	ResourcePool string `yaml:"resource_pool"`
	Datastore    string `yaml:"datastore"`
	// StoragePolicy is the name of a vSphere Storage Policy (SPBM) to apply to the
	// cloned VM (home and disks). Optional; when empty the datastore default policy is used.
	StoragePolicy string `yaml:"storage_policy"`
	Network       string `yaml:"network"`
	Template      string `yaml:"template"` // VM template name to clone from (inventory)
	// ContentLibrary + LibraryItem select a vSphere Content Library OVF item to
	// deploy from, as an alternative to cloning an inventory Template. Optional;
	// mutually exclusive with Template. See issue #25.
	ContentLibrary string `yaml:"content_library"`
	LibraryItem    string `yaml:"library_item"`
	// Firmware pins the VM's boot firmware to "bios" or "efi". Empty means inherit
	// from the template or OVF, which is the historical behavior. Talos OVAs from
	// factory.talos.dev import as BIOS, so "efi" is the usual choice. See issue #49.
	Firmware string `yaml:"firmware,omitempty"`
	Folder   string `yaml:"folder"`  // VM folder path (optional)
	CACert   string `yaml:"ca_cert"` // PEM-encoded CA certificate (optional)
	// ExtraConfig sets arbitrary vSphere Advanced Parameters on the VM, applied in
	// the order given. Values are strings as vCenter stores them ("TRUE", not true).
	// The provider-owned keys may not be set here. See issue #50.
	ExtraConfig []ExtraConfigItem `yaml:"extra_config,omitempty"`
	// Tags lists vCenter tags to attach to the VM after cloning. Each entry is a
	// tag name, or "category/name" when the tag name is not unique across
	// categories. All tags (and categories) must already exist in vCenter.
	Tags []string `yaml:"tags"`
	// PCIDevices lists physical PCI devices to pass through to the VM. Attaching any
	// device forces a full memory reservation, which vSphere requires for
	// passthrough. See issue #48.
	PCIDevices []PCIDevice `yaml:"pci_devices,omitempty"`
	DiskSize   uint64      `yaml:"disk_size"` // GiB
	CPU        uint        `yaml:"cpu"`
	Memory     uint        `yaml:"memory"` // MiB
	// ClusterFolder, when true, places the VM in a subfolder (under Folder, or the
	// datacenter VM folder) named after the cluster. The name is best-effort: it is
	// derived from the Omni machine request set ID by stripping the default
	// "-control-planes"/"-workers" machine set suffixes; custom-named machine sets
	// get a folder named after the full machine set ID. Missing folders are created.
	ClusterFolder bool `yaml:"cluster_folder"`
}

// Validate checks that the provider data selects exactly one VM source: an
// inventory Template, or a Content Library item (ContentLibrary + LibraryItem).
// It also checks the PCI passthrough and extraConfig settings.
func (d Data) Validate() error {
	usingTemplate := d.Template != ""
	usingLibrary := d.ContentLibrary != "" || d.LibraryItem != ""

	switch {
	case usingTemplate && usingLibrary:
		return errors.New("both template and content_library/library_item are set; they are mutually exclusive")
	case !usingTemplate && !usingLibrary:
		return errors.New("no VM source set: specify either template, or content_library + library_item")
	case usingLibrary && (d.ContentLibrary == "" || d.LibraryItem == ""):
		return errors.New("content_library and library_item must both be set")
	}

	if _, ok := firmwareType(d.Firmware); !ok {
		return fmt.Errorf("firmware %q is invalid: use \"bios\" or \"efi\"", d.Firmware)
	}

	for i, device := range d.PCIDevices {
		switch {
		case device.Label == "" && device.Address == "":
			return fmt.Errorf("pci_devices[%d]: set either label or address", i)
		case device.Label != "" && device.Address != "":
			return fmt.Errorf("pci_devices[%d]: label and address are mutually exclusive", i)
		}
	}

	seen := make(map[string]struct{}, len(d.ExtraConfig))

	for i, item := range d.ExtraConfig {
		switch item.Key {
		case "":
			return fmt.Errorf("extra_config[%d]: key is empty", i)
		case extraConfigKeyTalosConfig, extraConfigKeyDiskUUID:
			return fmt.Errorf("extra_config[%d]: key %q is managed by the provider and may not be set", i, item.Key)
		}

		if item.Value == "" {
			return fmt.Errorf("extra_config[%d]: value is empty", i)
		}

		if _, duplicate := seen[item.Key]; duplicate {
			return fmt.Errorf("extra_config[%d]: key %q is set more than once", i, item.Key)
		}

		seen[item.Key] = struct{}{}
	}

	return nil
}

// firmwareType maps the machine class firmware setting onto the vSphere value.
// "uefi" is accepted as an alias for "efi" because that is what the vCenter UI
// calls it. An empty setting maps to the zero value, meaning "leave the firmware
// alone and let vSphere inherit it". The second return is false for an
// unrecognized value.
func firmwareType(firmware string) (types.GuestOsDescriptorFirmwareType, bool) {
	switch strings.ToLower(firmware) {
	case "":
		return "", true
	case string(types.GuestOsDescriptorFirmwareTypeBios):
		return types.GuestOsDescriptorFirmwareTypeBios, true
	case string(types.GuestOsDescriptorFirmwareTypeEfi), "uefi":
		return types.GuestOsDescriptorFirmwareTypeEfi, true
	default:
		return "", false
	}
}

// applyFirmware pins the boot firmware on a config spec when the machine class sets
// one, and leaves the spec untouched otherwise so vSphere keeps inheriting the
// firmware from the template or OVF. Validate has already rejected any value
// firmwareType does not recognize.
func applyFirmware(spec *types.VirtualMachineConfigSpec, data Data) {
	firmware, ok := firmwareType(data.Firmware)
	if !ok || firmware == "" {
		return
	}

	spec.Firmware = string(firmware)
}

// extraConfigOptions builds the VM extraConfig: the provider-owned keys first, then
// Data.ExtraConfig in the order it was written. Validate has already rejected any
// attempt to override a provider-owned key.
func extraConfigOptions(data Data, talosConfigB64 string) []types.BaseOptionValue {
	options := make([]types.BaseOptionValue, 0, len(data.ExtraConfig)+2)

	options = append(
		options,
		&types.OptionValue{Key: extraConfigKeyDiskUUID, Value: "TRUE"},
		&types.OptionValue{Key: extraConfigKeyTalosConfig, Value: talosConfigB64},
	)

	for _, item := range data.ExtraConfig {
		options = append(options, &types.OptionValue{Key: item.Key, Value: item.Value})
	}

	return options
}
