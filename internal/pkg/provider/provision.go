// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package provider implements vsphere infra provider core.
package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
	"github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/stdpatches"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/fault"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/pbm"
	"github.com/vmware/govmomi/vim25/mo"
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
	p.logger.Debug(
		"re-login details",
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

// normalizePEMCert ensures a PEM certificate string uses proper newlines and
// 64-character base64 line wrapping. The Omni UI may deliver the cert as a
// single line with spaces in place of newlines; this function reconstructs
// the correct PEM block(s) in that case.
func normalizePEMCert(cert string) string {
	// Normalize CRLF and trim surrounding whitespace.
	cert = strings.ReplaceAll(cert, "\r\n", "\n")
	cert = strings.TrimSpace(cert)

	// Already has newlines – just ensure a single trailing newline.
	if strings.Contains(cert, "\n") {
		return strings.TrimRight(cert, "\n") + "\n"
	}

	// Single-line cert: spaces replaced newlines (e.g. from the Omni UI textarea).
	// Reconstruct each PEM block individually.
	var out strings.Builder

	const (
		beginPrefix  = "-----BEGIN "
		endPrefix    = "-----END "
		markerSuffix = "-----"
	)

	for len(cert) > 0 {
		if !strings.HasPrefix(cert, beginPrefix) {
			break
		}

		// Locate end of BEGIN line, e.g. "-----BEGIN CERTIFICATE-----".
		beginRest := cert[len(beginPrefix):]
		headerTailIdx := strings.Index(beginRest, markerSuffix)

		if headerTailIdx < 0 {
			break
		}

		headerEnd := len(beginPrefix) + headerTailIdx + len(markerSuffix)
		header := cert[:headerEnd]
		cert = strings.TrimLeft(cert[headerEnd:], " ")

		// Locate start of END line, e.g. "-----END CERTIFICATE-----".
		endStart := strings.Index(cert, endPrefix)
		if endStart < 0 {
			break
		}

		// Base64 body is everything before the END marker; strip spaces.
		body := strings.ReplaceAll(cert[:endStart], " ", "")

		// Locate end of END line.
		endRest := cert[endStart+len(endPrefix):]
		footerTailIdx := strings.Index(endRest, markerSuffix)

		if footerTailIdx < 0 {
			break
		}

		footerEnd := endStart + len(endPrefix) + footerTailIdx + len(markerSuffix)
		footer := cert[endStart:footerEnd]
		cert = strings.TrimLeft(cert[footerEnd:], " ")

		out.WriteString(header)
		out.WriteByte('\n')

		for len(body) > 64 {
			out.WriteString(body[:64])
			out.WriteByte('\n')

			body = body[64:]
		}

		if len(body) > 0 {
			out.WriteString(body)
			out.WriteByte('\n')
		}

		out.WriteString(footer)
		out.WriteByte('\n')
	}

	if out.Len() == 0 {
		// Couldn't parse; return as-is with a trailing newline.
		return strings.TrimRight(cert, "\n") + "\n"
	}

	return out.String()
}

// resolveStoragePolicyID looks up a vSphere Storage Policy (SPBM) by name and
// returns its profile ID, suitable for a VirtualMachineDefinedProfileSpec.
func (p *Provisioner) resolveStoragePolicyID(ctx context.Context, name string) (string, error) {
	pbmClient, err := pbm.NewClient(ctx, p.vsphereClient.Client)
	if err != nil {
		return "", fmt.Errorf("failed to create PBM client: %w", err)
	}

	profiles, err := pbmClient.ProfileMap(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load storage policy profiles: %w", err)
	}

	profile, ok := profiles.Name[name]
	if !ok {
		return "", fmt.Errorf("storage policy %q not found", name)
	}

	return profile.GetPbmProfile().ProfileId.UniqueId, nil
}

// diskProfileLocators reads the template's virtual disks and returns relocate
// disk locators that apply the given storage profile to each disk during a clone.
// The home-level VirtualMachineRelocateSpec.Profile does NOT cover the VMDKs, so
// per-disk locators are required for the storage policy to land on the disks.
func diskProfileLocators(
	ctx context.Context,
	template *object.VirtualMachine,
	datastoreRef types.ManagedObjectReference,
	profile []types.BaseVirtualMachineProfileSpec,
) ([]types.VirtualMachineRelocateSpecDiskLocator, error) {
	var templateMo mo.VirtualMachine
	if err := template.Properties(ctx, template.Reference(), []string{"config.hardware.device"}, &templateMo); err != nil {
		return nil, fmt.Errorf("failed to read template devices: %w", err)
	}

	if templateMo.Config == nil {
		return nil, fmt.Errorf("template has no hardware configuration available")
	}

	var locators []types.VirtualMachineRelocateSpecDiskLocator

	for _, dev := range templateMo.Config.Hardware.Device {
		if disk, ok := dev.(*types.VirtualDisk); ok {
			locators = append(locators, types.VirtualMachineRelocateSpecDiskLocator{
				DiskId:    disk.Key,
				Datastore: datastoreRef,
				Profile:   profile,
			})
		}
	}

	return locators, nil
}

// clusterFolderName derives a best-effort cluster name from an Omni machine
// request set ID. The machine request set ID equals the machine set ID
// ("<cluster>-<machine set suffix>"); the default control plane and worker
// suffixes are stripped to recover the cluster name. Custom-named machine sets
// keep the full ID, as the cluster name cannot be reliably separated from it.
func clusterFolderName(requestSetID string) string {
	for _, suffix := range []string{omni.ControlPlanesIDSuffix, omni.DefaultWorkersIDSuffix} {
		if cluster, ok := strings.CutSuffix(requestSetID, "-"+suffix); ok && cluster != "" {
			return cluster
		}
	}

	return requestSetID
}

// ensureSubfolder returns the child folder of parent with the given name,
// creating it when it does not exist. A creation race between concurrently
// provisioned machines of the same cluster is resolved by re-finding the folder.
func ensureSubfolder(ctx context.Context, finder *find.Finder, parent *object.Folder, name string) (*object.Folder, error) {
	childPath := path.Join(parent.InventoryPath, name)

	sub, err := finder.Folder(ctx, childPath)
	if err == nil {
		return sub, nil
	}

	var notFoundErr *find.NotFoundError
	if !errors.As(err, &notFoundErr) {
		return nil, fmt.Errorf("failed to look up folder %q: %w", childPath, err)
	}

	sub, err = parent.CreateFolder(ctx, name)
	if err != nil {
		if fault.Is(err, &types.DuplicateName{}) {
			return finder.Folder(ctx, childPath)
		}

		return nil, fmt.Errorf("failed to create folder %q: %w", childPath, err)
	}

	sub.SetInventoryPath(childPath)

	return sub, nil
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

				if err := data.Validate(); err != nil {
					return fmt.Errorf("invalid provider data: %w", err)
				}

				vmName := pctx.GetRequestID()

				logger.Info(
					"creating VM",
					zap.String("name", vmName),
					zap.String("datacenter", data.Datacenter),
					zap.String("resource_pool", data.ResourcePool),
					zap.String("network", data.Network),
					zap.String("datastore", data.Datastore),
					zap.Uint("cpu", data.CPU),
					zap.Uint("memory", data.Memory),
					zap.Uint64("disk_size", data.DiskSize),
					zap.String("storage_policy", data.StoragePolicy),
					zap.Bool("cluster_folder", data.ClusterFolder),
					zap.Bool("ca_cert_set", data.CACert != ""),
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

				// Optionally place the VM in a per-cluster subfolder, creating it if needed.
				if data.ClusterFolder {
					requestSetID, ok := pctx.GetMachineRequestSetID()
					if !ok {
						return fmt.Errorf("cluster_folder is enabled, but the machine request has no machine request set label")
					}

					folderName := clusterFolderName(requestSetID)

					logger.Info("using cluster folder", zap.String("name", vmName), zap.String("cluster_folder", folderName))

					folder, err = ensureSubfolder(ctx, finder, folder, folderName)
					if err != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to ensure cluster folder %q: %w", folderName, err)
					}
				}

				// Find the resource pool
				resourcePool, err := finder.ResourcePool(ctx, data.ResourcePool)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find resource pool %q: %w", data.ResourcePool, err)
				}

				// Find the datastore
				datastore, err := finder.Datastore(ctx, data.Datastore)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find datastore %q: %w", data.Datastore, err)
				}

				// Build clone spec
				resourcePoolRef := resourcePool.Reference()
				datastoreRef := datastore.Reference()

				// Prepare join config and hostname patches
				// We start first with creating a standard patch for the hostname.
				// With the standard patch, we'll create a machine-targeted config patch in Omni.
				// Then, if we're greater than talos v1.11, we'll combine it with the join config
				// to form a multi-document YAML that we'll pass to the VM via guestinfo.
				version := pctx.GetTalosVersion()

				versionContract, err := config.ParseContractFromVersion(version)
				if err != nil {
					return fmt.Errorf("failed to parse Talos contract from version. machine=%s version=%s", vmName, version)
				}

				hostnameConfig, err := stdpatches.WithStaticHostname(versionContract, vmName)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to create hostname config patch: %w", err)
				}

				err = pctx.CreateConfigPatch(ctx, fmt.Sprintf("000-hostname-%s", vmName), hostnameConfig)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to create hostname config patch in context: %w", err)
				}

				// Combine join config and hostname patch into multi-document YAML
				// if we support multi-doc. We also just concat strings here, as getting into unmarshalling
				// and remarshalling the specific patches felt like too much work for this. We may
				// have to go down that path later if we need to support other patches before joining to a cluster.
				var combinedConfig bytes.Buffer
				combinedConfig.WriteString(pctx.ConnectionParams.JoinConfig)
				combinedConfig.WriteString("---\n")
				combinedConfig.Write(hostnameConfig)

				// Handle custom CA certs if provided. We'll add these to the extra config, as well as generate a machine-scoped patch for it.
				if data.CACert != "" {
					caConfig, caErr := stdpatches.WithTrustedRoots(versionContract, normalizePEMCert(data.CACert))
					if caErr != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to create CA cert config patch: %w", caErr)
					}

					// Add CA cert to combined config YAML
					combinedConfig.WriteString("---\n")
					combinedConfig.Write(caConfig)

					// Generate machine-scoped patch for CA cert
					err = pctx.CreateConfigPatch(ctx, fmt.Sprintf("000-custom-ca-%s", vmName), caConfig)
					if err != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to create CA cert config patch in context: %w", err)
					}
				}

				combinedConfigB64 := base64.StdEncoding.EncodeToString(combinedConfig.Bytes())

				// Content Library deploy path: deploy from the OVF item, then apply the
				// config/cpu/memory/disk that the OVF deploy spec cannot carry cleanly.
				if data.LibraryItem != "" {
					vm, deployErr := p.deployFromContentLibrary(ctx, finder, data, vmName, resourcePool, folder, datastore, logger)
					if deployErr != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to deploy from content library: %w", deployErr)
					}

					if finalizeErr := p.finalizeVM(ctx, vm, data, combinedConfigB64, logger); finalizeErr != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to finalize VM: %w", finalizeErr)
					}

					pctx.State.TypedSpec().Value.VmName = vmName
					pctx.State.TypedSpec().Value.Datacenter = data.Datacenter

					return nil
				}

				// Template clone path: find the inventory template to clone from.
				template, err := finder.VirtualMachine(ctx, data.Template)
				if err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to find template %q: %w", data.Template, err)
				}

				// Resolve the optional storage policy (SPBM). When set, it is applied to
				// both the VM home (Location.Profile) and every disk (Location.Disk), as
				// the home profile alone does not cover the VMDKs during a clone.
				var (
					profileSpecs []types.BaseVirtualMachineProfileSpec
					diskLocators []types.VirtualMachineRelocateSpecDiskLocator
				)

				if data.StoragePolicy != "" {
					profileID, profileErr := p.resolveStoragePolicyID(ctx, data.StoragePolicy)
					if profileErr != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to resolve storage policy %q: %w", data.StoragePolicy, profileErr)
					}

					logger.Info(
						"applying storage policy",
						zap.String("name", vmName),
						zap.String("storage_policy", data.StoragePolicy),
						zap.String("profile_id", profileID),
					)

					profileSpecs = []types.BaseVirtualMachineProfileSpec{
						&types.VirtualMachineDefinedProfileSpec{ProfileId: profileID},
					}

					diskLocators, err = diskProfileLocators(ctx, template, datastoreRef, profileSpecs)
					if err != nil {
						return provision.NewRetryErrorf(time.Second*10, "failed to build disk profile locators: %w", err)
					}
				}

				// Clone the VM from template
				cloneSpec := types.VirtualMachineCloneSpec{
					Location: types.VirtualMachineRelocateSpec{
						Pool:      &resourcePoolRef,
						Datastore: &datastoreRef,
						Profile:   profileSpecs,
						Disk:      diskLocators,
					},
					Config: &types.VirtualMachineConfigSpec{
						NumCPUs:  int32(data.CPU),
						MemoryMB: int64(data.Memory),
						ExtraConfig: []types.BaseOptionValue{
							&types.OptionValue{Key: "disk.enableUUID", Value: "TRUE"},
							&types.OptionValue{Key: "guestinfo.talos.config", Value: combinedConfigB64},
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
			"attachTags",
			func(ctx context.Context, logger *zap.Logger, pctx provision.Context[*resources.Machine]) error {
				// Unmarshal provider-specific configuration
				var data Data
				if err := pctx.UnmarshalProviderData(&data); err != nil {
					return fmt.Errorf("failed to unmarshal provider data: %w", err)
				}

				if len(data.Tags) == 0 {
					return nil
				}

				// Ensure session is active
				if err := p.ensureSession(ctx); err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to ensure vSphere session: %w", err)
				}

				vmName := pctx.State.TypedSpec().Value.VmName
				if vmName == "" {
					return provision.NewRetryErrorf(time.Second*10, "waiting for VM to be created")
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

				logger.Info("attaching vCenter tags", zap.String("name", vmName), zap.Strings("tags", data.Tags))

				if err := p.attachTags(ctx, vm.Reference(), data.Tags); err != nil {
					return provision.NewRetryErrorf(time.Second*10, "failed to attach tags to VM %q: %w", vmName, err)
				}

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
