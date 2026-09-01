// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"go.uber.org/zap/zaptest"
)

// simEnv is the inventory a createVM-like test needs: a finder scoped to the
// simulator's datacenter, the default VM folder, and the refs a clone needs.
type simEnv struct {
	provisioner *Provisioner
	finder      *find.Finder
	folder      *object.Folder
	template    *object.VirtualMachine
}

func newSimEnv(ctx context.Context, t *testing.T, client *vim25.Client) simEnv {
	t.Helper()

	finder := find.NewFinder(client, true)

	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		t.Fatalf("failed to find datacenter: %v", err)
	}

	finder.SetDatacenter(dc)

	folder, err := finder.DefaultFolder(ctx)
	if err != nil {
		t.Fatalf("failed to find default folder: %v", err)
	}

	// The simulator seeds the inventory with VMs; any of them serves as a clone
	// source, which is all the provider asks of a template.
	templates, err := finder.VirtualMachineList(ctx, "*")
	if err != nil {
		t.Fatalf("failed to find a template VM: %v", err)
	}

	template := templates[0]

	return simEnv{
		provisioner: NewProvisioner(
			&govmomi.Client{Client: client, SessionManager: session.NewManager(client)},
			zaptest.NewLogger(t),
			nil,
		),
		finder:   finder,
		folder:   folder,
		template: template,
	}
}

// clone runs the provider's clone with the given data, named vmName.
func (env simEnv) clone(ctx context.Context, t *testing.T, data Data, vmName string) (*object.VirtualMachine, error) {
	t.Helper()

	// The simulator seeds several hosts and datastores, so pick the pool that already
	// backs the template rather than asking the finder for a unique default.
	pool, err := env.template.ResourcePool(ctx)
	if err != nil {
		t.Fatalf("failed to find resource pool: %v", err)
	}

	datastores, err := env.finder.DatastoreList(ctx, "*")
	if err != nil {
		t.Fatalf("failed to find datastore: %v", err)
	}

	datastore := datastores[0]

	data.Template = env.template.Name()

	return env.provisioner.cloneFromTemplate(
		ctx, env.finder, data, vmName, env.folder,
		pool.Reference(), datastore.Reference(), "cfg", zaptest.NewLogger(t),
	)
}

// TestFindExistingVM covers the lookup that makes createVM safe to retry: it must
// report "nothing there" for a name that was never created, and find the VM once it
// has been. Getting the first case wrong would strand every provision; getting the
// second wrong re-clones and fails on a duplicate name, which is the bug this
// guards against.
func TestFindExistingVM(t *testing.T) {
	simulator.Test(func(ctx context.Context, client *vim25.Client) {
		env := newSimEnv(ctx, t, client)

		vm, err := findExistingVM(ctx, env.finder, env.folder, "not-created-yet")
		if !errors.Is(err, errVMNotFound) {
			t.Fatalf("findExistingVM() on a missing VM returned %v, want errVMNotFound", err)
		}

		if vm != nil {
			t.Fatalf("findExistingVM() found %q, want nil for a VM that was never created", vm.Name())
		}

		const vmName = "test-machine"

		if _, err = env.clone(ctx, t, Data{CPU: 2, Memory: 4096}, vmName); err != nil {
			t.Fatalf("cloneFromTemplate() failed: %v", err)
		}

		vm, err = findExistingVM(ctx, env.finder, env.folder, vmName)
		if err != nil {
			t.Fatalf("findExistingVM() after the clone returned an error: %v", err)
		}

		if vm == nil {
			t.Fatal("findExistingVM() returned nil for a VM that was just cloned")
		}

		if vm.Name() != vmName {
			t.Errorf("findExistingVM() = %q, want %q", vm.Name(), vmName)
		}
	})
}

// vmFirmware reads config.firmware off a VM.
func vmFirmware(ctx context.Context, t *testing.T, vm *object.VirtualMachine) string {
	t.Helper()

	var vmMo mo.VirtualMachine

	if err := vm.Properties(ctx, vm.Reference(), []string{"config.firmware"}, &vmMo); err != nil {
		t.Fatalf("failed to read VM firmware: %v", err)
	}

	return vmMo.Config.Firmware
}

// TestEnsureFirmware checks that the machine class firmware lands on the created VM,
// that leaving it unset keeps whatever the source had, and that running it twice is
// a no-op - createVM is retried as a whole, so this runs again on every attempt.
func TestEnsureFirmware(t *testing.T) {
	for _, tc := range []struct {
		name     string
		firmware string
		vmName   string
	}{
		{"efi", "efi", "firmware-efi"},
		{"uefi is normalized to efi", "uefi", "firmware-uefi"},
		{"bios", "bios", "firmware-bios"},
		{"unset inherits from the template", "", "firmware-unset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			simulator.Test(func(ctx context.Context, client *vim25.Client) {
				env := newSimEnv(ctx, t, client)
				logger := zaptest.NewLogger(t)
				data := Data{CPU: 2, Memory: 4096, Firmware: tc.firmware}

				vm, err := env.clone(ctx, t, data, tc.vmName)
				if err != nil {
					t.Fatalf("cloneFromTemplate() failed: %v", err)
				}

				want := vmFirmware(ctx, t, vm)

				if firmware, _ := firmwareType(tc.firmware); firmware != "" {
					want = string(firmware)
				}

				if err = ensureFirmware(ctx, vm, data, logger); err != nil {
					t.Fatalf("ensureFirmware() failed: %v", err)
				}

				if got := vmFirmware(ctx, t, vm); got != want {
					t.Fatalf("VM firmware = %q, want %q", got, want)
				}

				// A second run must be a no-op rather than another reconfigure.
				if err = ensureFirmware(ctx, vm, data, logger); err != nil {
					t.Fatalf("second ensureFirmware() failed: %v", err)
				}

				if got := vmFirmware(ctx, t, vm); got != want {
					t.Errorf("VM firmware after a second run = %q, want %q", got, want)
				}
			})
		})
	}
}
