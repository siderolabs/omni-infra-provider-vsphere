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
}

// NewProvisioner creates a new provisioner.
func NewProvisioner(vsphereClient *govmomi.Client) *Provisioner {
	return &Provisioner{
		vsphereClient: vsphereClient,
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

				// Find the folder where VMs will be created (default to VM folder in datacenter)
				folder, err := finder.DefaultFolder(ctx)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find VM folder: %w", err)
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
				if err := task.Wait(ctx); err != nil {
					return provision.NewRetryErrorf(time.Second*10, "VM creation task failed: %w", err)
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
