// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

// Data is the provider custom machine config.
type Data struct {
	Datacenter   string `yaml:"datacenter"`
	ResourcePool string `yaml:"resource_pool"`
	Datastore    string `yaml:"datastore"`
	Network      string `yaml:"network"`
	Template     string `yaml:"template"`  // VM template name to clone from
	Folder       string `yaml:"folder"`    // VM folder path (optional)
	DiskSize     uint64 `yaml:"disk_size"` // GiB
	CPU          uint   `yaml:"cpu"`
	Memory       uint   `yaml:"memory"` // MiB
}
