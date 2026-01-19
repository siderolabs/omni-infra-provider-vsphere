// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package provider implements vsphere infra provider core.
package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"

	"github.com/siderolabs/omni-infra-provider-vsphere/internal/pkg/provider/resources"
)

const (
	GiB = uint64(1024 * 1024 * 1024)
)

// Provisioner implements Talos emulator infra provider.
type Provisioner struct {
	vsphereClient *govmomi.Client
	logger        *zap.Logger
	userInfo      *url.Userinfo
}

// ensureSession checks if the session is active and reconnects if needed.
func (p *Provisioner) ensureSession(ctx context.Context) error {
	active, err := p.vsphereClient.SessionManager.SessionIsActive(ctx)
	if err == nil && active {
		return nil
	}

	p.logger.Warn("vSphere session inactive, attempting re-login")
	p.logger.Debug("re-login details",
		zap.String("username", p.userInfo.Username()),
		zap.Bool("has_password", func() bool {
			_, ok := p.userInfo.Password()

			return ok
		}()),
	)

	// Logout first to clear any existing session
	if logoutErr := p.vsphereClient.SessionManager.Logout(ctx); logoutErr != nil {
		p.logger.Debug("logout before re-login failed (may be expected)", zap.Error(logoutErr))
	}

	if err := p.vsphereClient.SessionManager.Login(ctx, p.userInfo); err != nil {
		p.logger.Error("vSphere re-login failed", zap.Error(err))

		return err
	}

	p.logger.Info("vSphere session re-established")

	return nil
}

// resizeDisk resizes the first disk of the VM to the specified size in GiB.
func resizeDisk(ctx context.Context, vm *object.VirtualMachine, diskSizeGiB uint64) error {
	if diskSizeGiB == 0 {
		return nil
	}

	devices, err := vm.Device(ctx)
	if err != nil {
		return fmt.Errorf("failed to get VM devices: %w", err)
	}

	var disk *types.VirtualDisk
	for _, device := range devices {
		if d, ok := device.(*types.VirtualDisk); ok {
			disk = d

			break
		}
	}

	if disk == nil {
		return fmt.Errorf("no disk found on VM")
	}

	newCapacityKB := int64(diskSizeGiB * GiB / 1024)
	currentCapacityKB := disk.CapacityInKB

	if newCapacityKB <= currentCapacityKB {
		return nil
	}

	disk.CapacityInKB = newCapacityKB

	spec := types.VirtualMachineConfigSpec{
		DeviceChange: []types.BaseVirtualDeviceConfigSpec{
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationEdit,
				Device:    disk,
			},
		},
	}

	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to reconfigure VM for disk resize: %w", err)
	}

	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("disk resize task failed: %w", err)
	}

	return nil
}

// configureNetwork configures the VM's network adapter to use the specified network.
func configureNetwork(ctx context.Context, finder *find.Finder, vm *object.VirtualMachine, networkName string) error {
	if networkName == "" {
		return nil
	}

	network, err := finder.Network(ctx, networkName)
	if err != nil {
		return fmt.Errorf("failed to find network %q: %w", networkName, err)
	}

	devices, err := vm.Device(ctx)
	if err != nil {
		return fmt.Errorf("failed to get VM devices: %w", err)
	}

	var ethernetCard types.BaseVirtualEthernetCard
	for _, device := range devices {
		if card, ok := device.(types.BaseVirtualEthernetCard); ok {
			ethernetCard = card

			break
		}
	}

	if ethernetCard == nil {
		return fmt.Errorf("no network adapter found on VM")
	}

	backing, err := network.EthernetCardBackingInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get network backing info: %w", err)
	}

	ethernetCard.GetVirtualEthernetCard().Backing = backing

	device, ok := ethernetCard.(types.BaseVirtualDevice)
	if !ok {
		return fmt.Errorf("failed to convert ethernet card to base virtual device")
	}

	spec := types.VirtualMachineConfigSpec{
		DeviceChange: []types.BaseVirtualDeviceConfigSpec{
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationEdit,
				Device:    device,
			},
		},
	}

	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to reconfigure VM for network change: %w", err)
	}

	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("network configuration task failed: %w", err)
	}

	return nil
}

// NewProvisioner creates a new provisioner.
func NewProvisioner(vsphereClient *govmomi.Client, logger *zap.Logger, userInfo *url.Userinfo) *Provisioner {
	return &Provisioner{
		vsphereClient: vsphereClient,
		logger:        logger,
		userInfo:      userInfo,
	}
}

// ProvisionSteps implements infra.Provisioner.
//
//nolint:gocognit,gocyclo,cyclop,maintidx
func (p *Provisioner) ProvisionSteps() []provision.Step[*resources.Machine] {
	return []provision.Step[*resources.Machine]{
		provision.NewStep(
			"createVM",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				// Ensure session is active
				if err := p.ensureSession(ctx); err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to ensure vSphere session: %w", err)
				}

				// Unmarshal provider-specific configuration
				var data Data
				if err := pctx.UnmarshalProviderData(&data); err != nil {
					return fmt.Errorf("failed to unmarshal provider data: %w", err)
				}

				vmName := pctx.GetRequestID()

				logger.Info("creating VM",
					zap.String("name", vmName),
					zap.String("datacenter", data.Datacenter),
					zap.String("resource_pool", data.ResourcePool),
					zap.String("network", data.Network),
					zap.String("datastore", data.Datastore),
					zap.Uint("cpu", data.CPU),
					zap.Uint("memory", data.Memory),
					zap.Uint64("disk_size", data.DiskSize),
				)

				// Set up the finder with datacenter context
				finder := find.NewFinder(p.vsphereClient.Client, true)

				// Find the datacenter
				dc, err := finder.Datacenter(ctx, data.Datacenter)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find datacenter %q: %w", data.Datacenter, err)
				}

				finder.SetDatacenter(dc)

				// Find the folder where VMs will be created
				var folder *object.Folder
				if data.Folder != "" {
					// Use specified folder
					folder, err = finder.Folder(ctx, data.Folder)
					if err != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to find folder %q: %w", data.Folder, err)
					}
				} else {
					// Default to datacenter VM folder
					folder, err = finder.DefaultFolder(ctx)
					if err != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to find default VM folder: %w", err)
					}
				}

				// Find the resource pool
				resourcePool, err := finder.ResourcePool(ctx, data.ResourcePool)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find resource pool %q: %w", data.ResourcePool, err)
				}

				// Find the template VM
				template, err := finder.VirtualMachine(ctx, data.Template)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find template %q: %w", data.Template, err)
				}

				// Find the datastore
				datastore, err := finder.Datastore(ctx, data.Datastore)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find datastore %q: %w", data.Datastore, err)
				}

				// Build clone spec
				resourcePoolRef := resourcePool.Reference()
				datastoreRef := datastore.Reference()

				// Prepare join config userdata
				joinConfigBytes := []byte(pctx.ConnectionParams.JoinConfig)
				joinConfigB64 := base64.StdEncoding.EncodeToString(joinConfigBytes)

				// Clone the VM from template
				cloneSpec := types.VirtualMachineCloneSpec{
					Location: types.VirtualMachineRelocateSpec{
						Pool:      &resourcePoolRef,
						Datastore: &datastoreRef,
					},
					Config: &types.VirtualMachineConfigSpec{
						NumCPUs:  int32(data.CPU),
						MemoryMB: int64(data.Memory),
						ExtraConfig: []types.BaseOptionValue{
							&types.OptionValue{Key: "disk.enableUUID", Value: "TRUE"},
							&types.OptionValue{Key: "guestinfo.talos.config", Value: joinConfigB64},
						},
					},
					PowerOn: false,
				}

				task, err := template.Clone(ctx, folder, vmName, cloneSpec)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to clone VM from template: %w", err)
				}

				// Wait for the task to complete
				if waitErr := task.Wait(ctx); waitErr != nil {
					return provision.NewRetryErrorf(time.Second*10, "VM creation task failed: %w", waitErr)
				}

				// Find the newly created VM for disk resizing
				vm, err := finder.VirtualMachine(ctx, vmName)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find newly created VM %q: %w", vmName, err)
				}

				// Resize disk if specified
				if data.DiskSize > 0 {
					logger.Info("resizing VM disk", zap.String("name", vmName), zap.Uint64("disk_size_gib", data.DiskSize))

					if err := resizeDisk(ctx, vm, data.DiskSize); err != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to resize disk: %w", err)
					}
				}

				// Configure network if specified
				if data.Network != "" {
					logger.Info("configuring VM network", zap.String("name", vmName), zap.String("network", data.Network))

					if err := configureNetwork(ctx, finder, vm, data.Network); err != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to configure network: %w", err)
					}
				}

				// Store VM name, datacenter, and UUID in state
				pctx.State.TypedSpec().Value.VmName = vmName
				pctx.State.TypedSpec().Value.Datacenter = data.Datacenter

				return nil
			},
		),
		provision.NewStep(
			"powerOnVM",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				// Ensure session is active
				if err := p.ensureSession(ctx); err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to ensure vSphere session: %w", err)
				}

				vmName := pctx.State.TypedSpec().Value.VmName
				if vmName == "" {
					return provision.NewRetryErrorf(time.Second*10, "waiting for VM to be created")
				}

				// Unmarshal provider-specific configuration
				var data Data
				if err := pctx.UnmarshalProviderData(&data); err != nil {
					return fmt.Errorf("failed to unmarshal provider data: %w", err)
				}

				finder := find.NewFinder(p.vsphereClient.Client, true)

				// Find the datacenter
				dc, err := finder.Datacenter(ctx, data.Datacenter)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find datacenter %q: %w", data.Datacenter, err)
				}

				finder.SetDatacenter(dc)

				// Find the VM
				vm, err := finder.VirtualMachine(ctx, vmName)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find VM %q: %w", vmName, err)
				}

				// Check power state
				powerState, err := vm.PowerState(ctx)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to get VM power state: %w", err)
				}

				if powerState == types.VirtualMachinePowerStatePoweredOn {
					logger.Info("VM is already powered on", zap.String("name", vmName))

					return nil
				}

				logger.Info("powering on VM", zap.String("name", vmName))

				// Power on the VM
				task, err := vm.PowerOn(ctx)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to power on VM: %w", err)
				}

				if err := task.Wait(ctx); err != nil {
					return provision.NewRetryErrorf(time.Second*10, "power on task failed: %w", err)
				}

				logger.Info("VM powered on successfully", zap.String("name", vmName))

				return nil
			},
		),
	}
}

// Deprovision implements infra.Provisioner.
func (p *Provisioner) Deprovision(ctx context.Context, logger *zap.Logger, machine *resources.Machine, machineRequest *infra.MachineRequest) error {
	// Ensure session is active
	if err := p.ensureSession(ctx); err != nil {
		return fmt.Errorf("failed to ensure vSphere session: %w", err)
	}

	vmName := machineRequest.Metadata().ID()

	if vmName == "" {
		return fmt.Errorf("empty vmName")
	}

	logger.Info("deprovisioning VM", zap.String("name", vmName))

	// Get datacenter from machine state
	datacenter := machine.TypedSpec().Value.Datacenter
	if datacenter == "" {
		// If there is no datacenter info, it could mean that machine
		// provisioning failed early and we shouldn't attempt to
		// remove the machine from vsphere.
		logger.Info("datacenter not found in machine state")

		return nil
	}

	finder := find.NewFinder(p.vsphereClient.Client, true)

	// Find the datacenter
	dc, err := finder.Datacenter(ctx, datacenter)
	if err != nil {
		return fmt.Errorf("failed to find datacenter %q: %w", datacenter, err)
	}

	finder.SetDatacenter(dc)

	// Find the VM
	vm, err := finder.VirtualMachine(ctx, vmName)
	if err != nil {
		// Only ignore "not found" errors - VM already deleted
		var notFoundErr *find.NotFoundError
		if errors.As(err, &notFoundErr) {
			logger.Info("VM not found, already removed", zap.String("name", vmName))

			return nil
		}

		return fmt.Errorf("failed to find VM %q: %w", vmName, err)
	}

	logger.Info("found VM", zap.String("name", vmName))

	// Check power state and power off if needed
	powerState, err := vm.PowerState(ctx)
	if err != nil {
		return fmt.Errorf("failed to get VM power state: %w", err)
	}

	if powerState == types.VirtualMachinePowerStatePoweredOn {
		logger.Info("powering off VM", zap.String("name", vmName))

		var task *object.Task

		task, err = vm.PowerOff(ctx)
		if err != nil {
			return fmt.Errorf("failed to power off VM: %w", err)
		}

		if err = task.Wait(ctx); err != nil {
			return fmt.Errorf("power off task failed: %w", err)
		}

		logger.Info("VM powered off", zap.String("name", vmName))
	}

	// Destroy (delete) the VM
	logger.Info("destroying VM", zap.String("name", vmName))

	task, err := vm.Destroy(ctx)
	if err != nil {
		return fmt.Errorf("failed to destroy VM: %w", err)
	}

	if err = task.Wait(ctx); err != nil {
		return fmt.Errorf("destroy task failed: %w", err)
	}

	logger.Info("VM destroyed successfully", zap.String("name", vmName))

	return nil
}
