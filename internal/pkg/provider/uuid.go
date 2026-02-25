// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// convertVSphereUUIDToTalosFormat converts a vSphere UUID to the format that Talos reports.
// vSphere UUIDs have the first 3 groups byte-swapped compared to what Talos reports.
//
// Example:
//   vSphere: 422413c3-57c8-96d1-c481-c58dbb837d2d
//   Talos:   c3132442-c857-d196-c481-c58dbb837d2d
//
// The last two groups (clock_seq and node) remain the same.
func convertVSphereUUIDToTalosFormat(vsphereUUID string) (string, error) {
	// Remove hyphens and validate length
	uuid := strings.ReplaceAll(vsphereUUID, "-", "")
	if len(uuid) != 32 {
		return "", fmt.Errorf("invalid UUID length: %d", len(uuid))
	}

	// Decode hex string to bytes
	bytes, err := hex.DecodeString(uuid)
	if err != nil {
		return "", fmt.Errorf("invalid UUID hex: %w", err)
	}

	// Swap bytes in first 3 groups (time_low, time_mid, time_hi_and_version)
	// Group 1 (time_low): bytes 0-3 -> reverse
	bytes[0], bytes[1], bytes[2], bytes[3] = bytes[3], bytes[2], bytes[1], bytes[0]
	// Group 2 (time_mid): bytes 4-5 -> reverse
	bytes[4], bytes[5] = bytes[5], bytes[4]
	// Group 3 (time_hi_and_version): bytes 6-7 -> reverse
	bytes[6], bytes[7] = bytes[7], bytes[6]
	// Groups 4-5 (clock_seq and node): bytes 8-15 -> no change

	// Format as UUID string
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(bytes[0])<<24|uint32(bytes[1])<<16|uint32(bytes[2])<<8|uint32(bytes[3]),
		uint16(bytes[4])<<8|uint16(bytes[5]),
		uint16(bytes[6])<<8|uint16(bytes[7]),
		uint16(bytes[8])<<8|uint16(bytes[9]),
		uint64(bytes[10])<<40|uint64(bytes[11])<<32|uint64(bytes[12])<<24|uint64(bytes[13])<<16|uint64(bytes[14])<<8|uint64(bytes[15]),
	), nil
}
