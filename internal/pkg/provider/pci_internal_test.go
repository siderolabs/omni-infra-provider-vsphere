// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"testing"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap/zaptest"
)

func TestPCIDeviceKey(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		device PCIDevice
		want   string
	}{
		{"label", PCIDevice{Label: "gpu"}, "label:gpu"},
		{"address", PCIDevice{Address: "0000:06:00.0"}, "address:0000:06:00.0"},
	} {
		if got := pciDeviceKey(tc.device); got != tc.want {
			t.Errorf("%s: pciDeviceKey() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAttachedPCIDeviceKeys(t *testing.T) {
	t.Parallel()

	devices := object.VirtualDeviceList{
		&types.VirtualDisk{},
		&types.VirtualPCIPassthrough{
			VirtualDevice: types.VirtualDevice{
				Backing: &types.VirtualPCIPassthroughDynamicBackingInfo{CustomLabel: "gpu"},
			},
		},
		&types.VirtualPCIPassthrough{
			VirtualDevice: types.VirtualDevice{
				Backing: &types.VirtualPCIPassthroughDeviceBackingInfo{Id: "0000:06:00.0"},
			},
		},
	}

	attached := attachedPCIDeviceKeys(devices)

	for _, key := range []string{"label:gpu", "address:0000:06:00.0"} {
		if _, ok := attached[key]; !ok {
			t.Errorf("attachedPCIDeviceKeys() missing %q, got %v", key, attached)
		}
	}

	if len(attached) != 2 {
		t.Errorf("attachedPCIDeviceKeys() = %v, want 2 entries", attached)
	}
}

func TestPCIBacking(t *testing.T) {
	t.Parallel()

	target := &types.ConfigTarget{
		DynamicPassthrough: []types.VirtualMachineDynamicPassthroughInfo{
			{CustomLabel: "gpu"},
		},
		PciPassthrough: []types.BaseVirtualMachinePciPassthroughInfo{
			&types.VirtualMachinePciPassthroughInfo{
				PciDevice: types.HostPciDevice{Id: "0000:06:00.0", VendorId: 0x10de, DeviceId: 0x1eb8},
				SystemId:  "host-system",
			},
		},
	}

	logger := zaptest.NewLogger(t)

	t.Run("label present on host", func(t *testing.T) {
		t.Parallel()

		backing, err := pciBacking(target, PCIDevice{Label: "gpu"}, logger)
		if err != nil {
			t.Fatalf("pciBacking() failed: %v", err)
		}

		dynamic, ok := backing.(*types.VirtualPCIPassthroughDynamicBackingInfo)
		if !ok || dynamic.CustomLabel != "gpu" {
			t.Fatalf("pciBacking() = %#v, want dynamic backing for label %q", backing, "gpu")
		}
	})

	t.Run("label missing from host is a warning, not an error", func(t *testing.T) {
		t.Parallel()

		backing, err := pciBacking(target, PCIDevice{Label: "missing"}, logger)
		if err != nil {
			t.Fatalf("pciBacking() failed: %v", err)
		}

		dynamic, ok := backing.(*types.VirtualPCIPassthroughDynamicBackingInfo)
		if !ok || dynamic.CustomLabel != "missing" {
			t.Fatalf("pciBacking() = %#v, want dynamic backing for label %q", backing, "missing")
		}
	})

	t.Run("address present on host", func(t *testing.T) {
		t.Parallel()

		backing, err := pciBacking(target, PCIDevice{Address: "0000:06:00.0"}, logger)
		if err != nil {
			t.Fatalf("pciBacking() failed: %v", err)
		}

		device, ok := backing.(*types.VirtualPCIPassthroughDeviceBackingInfo)
		if !ok || device.Id != "0000:06:00.0" || device.SystemId != "host-system" {
			t.Fatalf("pciBacking() = %#v, want device backing for 0000:06:00.0 on host-system", backing)
		}
	})

	t.Run("address missing from host is a hard error", func(t *testing.T) {
		t.Parallel()

		if _, err := pciBacking(target, PCIDevice{Address: "0000:07:00.0"}, logger); err == nil {
			t.Fatal("pciBacking() succeeded for an address not offered by the host, want error")
		}
	})
}

// TestAttachPCIDevices checks that attaching devices reserves all guest memory and
// that a retry - the step that calls this is retried as a whole - does not attach the
// same device twice. The simulator does not populate QueryConfigTarget with any PCI
// hardware, so this exercises the label path, which only warns rather than failing
// when the host offers no matching device.
func TestAttachPCIDevices(t *testing.T) {
	t.Parallel()

	simulator.Test(func(ctx context.Context, client *vim25.Client) {
		env := newSimEnv(ctx, t, client)
		logger := zaptest.NewLogger(t)

		data := Data{CPU: 2, Memory: 4096, PCIDevices: []PCIDevice{{Label: "gpu"}}}

		vm, err := env.clone(ctx, t, data, "pci-device-vm")
		if err != nil {
			t.Fatalf("cloneFromTemplate() failed: %v", err)
		}

		if err = attachPCIDevices(ctx, vm, data, logger); err != nil {
			t.Fatalf("attachPCIDevices() failed: %v", err)
		}

		var vmMo mo.VirtualMachine

		if err = vm.Properties(ctx, vm.Reference(), []string{"config.hardware.device", "config.memoryAllocation"}, &vmMo); err != nil {
			t.Fatalf("failed to read VM properties: %v", err)
		}

		attached := attachedPCIDeviceKeys(object.VirtualDeviceList(vmMo.Config.Hardware.Device))
		if _, ok := attached["label:gpu"]; !ok {
			t.Fatalf("VM devices %v, want a passthrough device for label %q", attached, "gpu")
		}

		if vmMo.Config.MemoryAllocation.Reservation == nil || *vmMo.Config.MemoryAllocation.Reservation != int64(data.Memory) {
			t.Fatalf("memory reservation = %v, want %d", vmMo.Config.MemoryAllocation.Reservation, data.Memory)
		}

		// A retry must not attach the same device a second time.
		if err = attachPCIDevices(ctx, vm, data, logger); err != nil {
			t.Fatalf("second attachPCIDevices() failed: %v", err)
		}

		if err = vm.Properties(ctx, vm.Reference(), []string{"config.hardware.device"}, &vmMo); err != nil {
			t.Fatalf("failed to read VM properties: %v", err)
		}

		count := 0

		for range object.VirtualDeviceList(vmMo.Config.Hardware.Device).SelectByType((*types.VirtualPCIPassthrough)(nil)) {
			count++
		}

		if count != 1 {
			t.Fatalf("VM has %d passthrough devices after a retry, want 1", count)
		}
	})
}
