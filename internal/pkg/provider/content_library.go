// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vapi/library"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/vcenter"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"
)

// deployFromContentLibrary deploys a VM from a vSphere Content Library OVF item
// (Data.ContentLibrary / Data.LibraryItem) via the vAPI, as an alternative to
// cloning an inventory VM Template. It returns the created VM, powered off.
//
// Unlike the clone path, the OVF deploy spec cannot carry the Talos machine
// config (vCenter only honors extra-config keys the OVF pre-declares), so the
// guestinfo config and the CPU/memory/disk overrides are applied afterward by
// finalizeVM. Network and, when set, the storage policy are applied here at
// deploy time. disk.EnableUUID is baked into the Talos vmware OVA already.
func (p *Provisioner) deployFromContentLibrary(
	ctx context.Context,
	finder *find.Finder,
	data Data,
	vmName string,
	resourcePool *object.ResourcePool,
	folder *object.Folder,
	datastore *object.Datastore,
	logger *zap.Logger,
) (*object.VirtualMachine, error) {
	// The Content Library deploy uses the vAPI (REST), which needs its own
	// session alongside the existing vim25/SOAP client.
	rc := rest.NewClient(p.vsphereClient.Client)
	if err := rc.Login(ctx, p.userInfo); err != nil {
		return nil, fmt.Errorf("failed to log in to vAPI REST session: %w", err)
	}

	defer func() {
		if err := rc.Logout(ctx); err != nil {
			logger.Debug("vAPI REST logout failed", zap.Error(err))
		}
	}()

	libMgr := library.NewManager(rc)

	lib, err := libMgr.GetLibraryByName(ctx, data.ContentLibrary)
	if err != nil {
		return nil, fmt.Errorf("failed to find content library %q: %w", data.ContentLibrary, err)
	}

	itemIDs, err := libMgr.FindLibraryItems(ctx, library.FindItem{LibraryID: lib.ID, Name: data.LibraryItem})
	if err != nil {
		return nil, fmt.Errorf("failed to find library item %q in %q: %w", data.LibraryItem, data.ContentLibrary, err)
	}

	if len(itemIDs) != 1 {
		return nil, fmt.Errorf("expected exactly one library item named %q in %q, found %d", data.LibraryItem, data.ContentLibrary, len(itemIDs))
	}

	itemID := itemIDs[0]

	resourcePoolID := resourcePool.Reference().Value

	deploySpec := vcenter.DeploymentSpec{
		Name:                vmName,
		AcceptAllEULA:       true,
		DefaultDatastoreID:  datastore.Reference().Value,
		StorageProvisioning: "thin",
	}

	// Map every network the OVF declares onto the requested network. The OVF
	// network identifiers are discovered from the deployment filter.
	if data.Network != "" {
		network, netErr := finder.Network(ctx, data.Network)
		if netErr != nil {
			return nil, fmt.Errorf("failed to find network %q: %w", data.Network, netErr)
		}

		networkID := network.Reference().Value

		filter, filterErr := vcenter.NewManager(rc).FilterLibraryItem(ctx, itemID, vcenter.FilterRequest{
			Target: vcenter.Target{ResourcePoolID: resourcePoolID},
		})
		if filterErr != nil {
			return nil, fmt.Errorf("failed to inspect OVF networks for %q: %w", data.LibraryItem, filterErr)
		}

		for _, ovfNetwork := range filter.Networks {
			deploySpec.NetworkMappings = append(deploySpec.NetworkMappings, vcenter.NetworkMapping{
				Key:   ovfNetwork,
				Value: networkID,
			})
		}
	}

	// Optional storage policy (SPBM). Unlike the clone path, the OVF deploy
	// spec's storage_profile_id covers the whole deployment (home and disks),
	// so no per-disk locators are needed here.
	if data.StoragePolicy != "" {
		profileID, policyErr := p.resolveStoragePolicyID(ctx, data.StoragePolicy)
		if policyErr != nil {
			return nil, fmt.Errorf("failed to resolve storage policy %q: %w", data.StoragePolicy, policyErr)
		}

		logger.Info(
			"applying storage policy",
			zap.String("name", vmName),
			zap.String("storage_policy", data.StoragePolicy),
			zap.String("profile_id", profileID),
		)

		deploySpec.StorageProfileID = profileID
	}

	deploy := vcenter.Deploy{
		DeploymentSpec: deploySpec,
		Target: vcenter.Target{
			ResourcePoolID: resourcePoolID,
			FolderID:       folder.Reference().Value,
		},
	}

	logger.Info(
		"deploying VM from content library",
		zap.String("name", vmName),
		zap.String("content_library", data.ContentLibrary),
		zap.String("library_item", data.LibraryItem),
	)

	ref, err := vcenter.NewManager(rc).DeployLibraryItem(ctx, itemID, deploy)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy library item %q: %w", data.LibraryItem, err)
	}

	if ref == nil {
		return nil, fmt.Errorf("content library deploy of %q returned no VM reference", data.LibraryItem)
	}

	return object.NewVirtualMachine(p.vsphereClient.Client, *ref), nil
}

// finalizeVM applies the settings an OVF deploy does not cover: the Talos
// machine config (guestinfo), the CPU/memory override, and the disk resize. It
// mirrors what the clone path bakes into the clone spec, injected here via a
// post-deploy reconfigure. disk.enableUUID is set as well for parity, though the
// Talos vmware OVA already bakes it in.
func (p *Provisioner) finalizeVM(
	ctx context.Context,
	vm *object.VirtualMachine,
	data Data,
	combinedConfigB64 string,
	logger *zap.Logger,
) error {
	spec := types.VirtualMachineConfigSpec{
		NumCPUs:  int32(data.CPU),
		MemoryMB: int64(data.Memory),
		ExtraConfig: []types.BaseOptionValue{
			&types.OptionValue{Key: "disk.enableUUID", Value: "TRUE"},
			&types.OptionValue{Key: "guestinfo.talos.config", Value: combinedConfigB64},
		},
	}

	logger.Info(
		"finalizing VM (config, cpu, memory)",
		zap.String("name", vm.Name()),
		zap.Uint("cpu", data.CPU),
		zap.Uint("memory", data.Memory),
	)

	task, err := vm.Reconfigure(ctx, spec)
	if err != nil {
		return fmt.Errorf("failed to reconfigure VM: %w", err)
	}

	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("VM reconfigure task failed: %w", err)
	}

	if data.DiskSize > 0 {
		logger.Info("resizing VM disk", zap.String("name", vm.Name()), zap.Uint64("disk_size_gib", data.DiskSize))

		if err := resizeDisk(ctx, vm, data.DiskSize); err != nil {
			return fmt.Errorf("failed to resize disk: %w", err)
		}
	}

	return nil
}
