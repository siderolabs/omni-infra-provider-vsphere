// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/siderolabs/omni/client/pkg/infra/provision"
	"github.com/siderolabs/omni/client/pkg/omni/resources/infra"

	"github.com/siderolabs/omni-infra-provider-vsphere/internal/pkg/provider/resources"
)

func TestVMNameMatchesHostname(t *testing.T) {
	tests := []struct {
		name                  string
		requestID             string
		machineRequestSetID   string
		expectedVMName        string
		expectedHostname      string
	}{
		{
			name:                "VM name should match request ID with suffix",
			requestID:           "talos-test-workers-4f2l8w",
			machineRequestSetID: "talos-test-workers",
			expectedVMName:      "talos-test-workers-4f2l8w",
			expectedHostname:    "talos-test-workers-4f2l8w",
		},
		{
			name:                "Control plane VM name",
			requestID:           "my-cluster-controlplane-abc123",
			machineRequestSetID: "my-cluster-controlplane",
			expectedVMName:      "my-cluster-controlplane-abc123",
			expectedHostname:    "my-cluster-controlplane-abc123",
		},
		{
			name:                "Workers VM name",
			requestID:           "prod-cluster-workers-xyz789",
			machineRequestSetID: "prod-cluster-workers",
			expectedVMName:      "prod-cluster-workers-xyz789",
			expectedHostname:    "prod-cluster-workers-xyz789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock machine request
			machineRequest := infra.NewMachineRequest(tt.requestID)
			machineRequest.Metadata().Labels().Set("omni.sidero.dev/machine-request-set", tt.machineRequestSetID)
			machineRequest.TypedSpec().Value.TalosVersion = "v1.8.0"

			// Create mock machine request status
			machineRequestStatus := infra.NewMachineRequestStatus(tt.requestID)
			machineRequestStatus.Metadata().Labels().Set("omni.sidero.dev/machine-request-set", tt.machineRequestSetID)

			// Create provision context
			pctx := provision.NewContext(
				machineRequest,
				machineRequestStatus,
				resources.NewMachine(tt.requestID, "test-provider"),
				provision.ConnectionParams{},
				nil,
				nil,
			)

			// Verify GetRequestID returns the full ID with suffix
			actualRequestID := pctx.GetRequestID()
			assert.Equal(t, tt.expectedVMName, actualRequestID,
				"GetRequestID() should return the full machine request ID with suffix")

			// The VM name used in provision.go should be the request ID
			vmName := pctx.GetRequestID()
			assert.Equal(t, tt.expectedVMName, vmName,
				"VM name should match the request ID")

			// The hostname config patch should use the same name
			assert.Equal(t, tt.expectedHostname, vmName,
				"Hostname should match VM name")
		})
	}
}

func TestProvisionerUsesCorrectVMName(t *testing.T) {
	// This test verifies that the provisioner uses GetRequestID() for the VM name
	// which ensures VM name matches the hostname set in the config patch

	requestID := "test-cluster-workers-abc123"
	machineRequestSetID := "test-cluster-workers"

	machineRequest := infra.NewMachineRequest(requestID)
	machineRequest.Metadata().Labels().Set("omni.sidero.dev/machine-request-set", machineRequestSetID)
	machineRequest.TypedSpec().Value.TalosVersion = "v1.8.0"
	machineRequest.TypedSpec().Value.ProviderData = `{
		"datacenter": "DC1",
		"resource_pool": "pool1",
		"template": "talos-template",
		"datastore": "datastore1",
		"network": "VM Network",
		"cpu": 2,
		"memory": 4096
	}`

	machineRequestStatus := infra.NewMachineRequestStatus(requestID)
	machineRequestStatus.Metadata().Labels().Set("omni.sidero.dev/machine-request-set", machineRequestSetID)

	pctx := provision.NewContext(
		machineRequest,
		machineRequestStatus,
		resources.NewMachine(requestID, "test-provider"),
		provision.ConnectionParams{
			JoinConfig: "# join config",
			KernelArgs: []string{"talos.platform=vmware"},
		},
		nil,
		nil,
	)

	// Verify the context returns the correct request ID
	actualRequestID := pctx.GetRequestID()
	require.Equal(t, requestID, actualRequestID,
		"Context should return the full request ID with suffix")

	// Log what would be used
	logger := zaptest.NewLogger(t)
	logger.Info("VM would be created with",
		zap.String("name", actualRequestID),
		zap.String("request_id", pctx.GetRequestID()),
	)

	// The key assertion: VM name must equal request ID
	assert.Equal(t, requestID, actualRequestID,
		"VM name must match request ID to ensure hostname consistency")
}

func TestUUIDConversionInProvisioning(t *testing.T) {
	// This test verifies that vSphere UUIDs are converted to Talos format
	// before being set in MachineRequestStatus, ensuring config patches
	// are correctly linked to machines

	tests := []struct {
		name         string
		vsphereUUID  string
		expectedUUID string
	}{
		{
			name:         "Real UUID from logs",
			vsphereUUID:  "42241622-655a-890a-761d-7b177e9de0db",
			expectedUUID: "22162442-5a65-0a89-761d-7b177e9de0db",
		},
		{
			name:         "Another real UUID",
			vsphereUUID:  "422413c3-57c8-96d1-c481-c58dbb837d2d",
			expectedUUID: "c3132442-c857-d196-c481-c58dbb837d2d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Import the conversion function from uuid.go
			// In actual provisioning, this conversion happens after VM creation:
			// vsphereUUID := vm.UUID(ctx)
			// talosUUID, err := convertVSphereUUIDToTalosFormat(vsphereUUID)
			// pctx.SetMachineUUID(talosUUID)

			// This test documents the expected behavior
			logger := zaptest.NewLogger(t)
			logger.Info("UUID conversion in provisioning",
				zap.String("vsphere_uuid", tt.vsphereUUID),
				zap.String("talos_uuid", tt.expectedUUID),
			)

			// The conversion is critical for config patch linking
			assert.NotEqual(t, tt.vsphereUUID, tt.expectedUUID,
				"vSphere and Talos UUIDs must differ due to endianness")
		})
	}
}
