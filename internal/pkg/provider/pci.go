// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"
)

// pciDeviceKey identifies a passthrough device by the backing it selects, so
// already-attached devices can be told apart from the ones still to add. The
// prefix keeps a label from ever colliding with an address.
func pciDeviceKey(device PCIDevice) string {
	if device.Label != "" {
		return "label:" + device.Label
	}

	return "address:" + device.Address
}

// attachedPCIDeviceKeys returns the keys of the passthrough devices already on the
// VM. The step that calls this is retryable, so a partially applied reconfigure
// must not lead to the same device being added twice.
func attachedPCIDeviceKeys(devices object.VirtualDeviceList) map[string]struct{} {
	attached := map[string]struct{}{}

	for _, device := range devices.SelectByType((*types.VirtualPCIPassthrough)(nil)) {
		passthrough, ok := device.(*types.VirtualPCIPassthrough)
		if !ok {
			continue
		}

		switch backing := passthrough.Backing.(type) {
		case *types.VirtualPCIPassthroughDynamicBackingInfo:
			attached[pciDeviceKey(PCIDevice{Label: backing.CustomLabel})] = struct{}{}
		case *types.VirtualPCIPassthroughDeviceBackingInfo:
			attached[pciDeviceKey(PCIDevice{Address: backing.Id})] = struct{}{}
		}
	}

	return attached
}

// passthroughTarget returns the passthrough devices the VM's host offers, which is
// where the vendor/device/system IDs needed for a static backing come from.
func passthroughTarget(ctx context.Context, vm *object.VirtualMachine) (*types.ConfigTarget, error) {
	browser, err := vm.EnvironmentBrowser(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get environment browser: %w", err)
	}

	target, err := browser.QueryConfigTarget(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query host config target: %w", err)
	}

	if target == nil {
		return nil, fmt.Errorf("host config target is empty")
	}

	return target, nil
}

// pciBacking builds the backing that maps a virtual passthrough device onto a
// physical one.
//
// A label uses Dynamic DirectPath I/O: vCenter matches the label against the host
// it places the VM on at power-on, so a label missing from the current host's
// target is only a warning - another host may serve it.
//
// An address uses static DirectPath I/O, which needs the device's vendor, device
// and system IDs. Those can only come from the host currently backing the VM, so
// an address that host does not offer is a hard error.
func pciBacking(target *types.ConfigTarget, device PCIDevice, logger *zap.Logger) (types.BaseVirtualDeviceBackingInfo, error) {
	if device.Label != "" {
		found := false

		for _, dynamic := range target.DynamicPassthrough {
			if dynamic.CustomLabel == device.Label {
				found = true

				break
			}
		}

		if !found {
			logger.Warn(
				"no PCI device with this hardware label is available on the VM's current host; "+
					"relying on vCenter to place the VM on a host that has one",
				zap.String("label", device.Label),
			)
		}

		return &types.VirtualPCIPassthroughDynamicBackingInfo{CustomLabel: device.Label}, nil
	}

	for _, info := range target.PciPassthrough {
		pci := info.GetVirtualMachinePciPassthroughInfo()
		if pci == nil || pci.PciDevice.Id != device.Address {
			continue
		}

		return &types.VirtualPCIPassthroughDeviceBackingInfo{
			Id:       pci.PciDevice.Id,
			DeviceId: fmt.Sprintf("%x", pci.PciDevice.DeviceId),
			SystemId: pci.SystemId,
			VendorId: pci.PciDevice.VendorId,
		}, nil
	}

	return nil, fmt.Errorf("PCI device %q is not available for passthrough on the VM's host", device.Address)
}

// attachPCIDevices attaches the configured PCI devices to the VM, which must be
// powered off. vSphere refuses to power on a VM with passthrough unless all guest
// memory is reserved, so the same reconfigure locks the reservation to the
// configured memory size.
func attachPCIDevices(ctx context.Context, vm *object.VirtualMachine, data Data, logger *zap.Logger) error {
	devices, err := vm.Device(ctx)
	if err != nil {
		return fmt.Errorf("failed to get VM devices: %w", err)
	}

	attached := attachedPCIDeviceKeys(devices)

	missing := make([]PCIDevice, 0, len(data.PCIDevices))

	for _, device := range data.PCIDevices {
		if _, ok := attached[pciDeviceKey(device)]; !ok {
			missing = append(missing, device)
		}
	}

	if len(missing) == 0 {
		logger.Info("PCI devices already attached", zap.String("name", vm.Name()))

		return nil
	}

	target, err := passthroughTarget(ctx, vm)
	if err != nil {
		return err
	}

	changes := make([]types.BaseVirtualDeviceConfigSpec, 0, len(missing))

	for i, device := range missing {
		backing, backingErr := pciBacking(target, device, logger)
		if backingErr != nil {
			return backingErr
		}

		changes = append(changes, &types.VirtualDeviceConfigSpec{
			Operation: types.VirtualDeviceConfigSpecOperationAdd,
			Device: &types.VirtualPCIPassthrough{
				VirtualDevice: types.VirtualDevice{
					// Negative keys are placeholders for devices that do not exist yet;
					// vCenter assigns the real keys and the PCI controller slot.
					Key:         int32(-(i + 1)),
					Backing:     backing,
					Connectable: &types.VirtualDeviceConnectInfo{StartConnected: true, Connected: true},
				},
			},
		})
	}

	reservation := int64(data.Memory)

	logger.Info(
		"attaching PCI devices and reserving all guest memory",
		zap.String("name", vm.Name()),
		zap.Int("devices", len(changes)),
		zap.Int64("memory_reservation_mib", reservation),
	)

	task, err := vm.Reconfigure(ctx, types.VirtualMachineConfigSpec{
		DeviceChange:                 changes,
		MemoryReservationLockedToMax: new(true),
		MemoryAllocation:             &types.ResourceAllocationInfo{Reservation: &reservation},
	})
	if err != nil {
		return fmt.Errorf("failed to reconfigure VM for PCI passthrough: %w", err)
	}

	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("PCI passthrough reconfigure task failed: %w", err)
	}

	return nil
}
