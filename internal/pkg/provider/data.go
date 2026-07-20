// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import "errors"

// Data is the provider custom machine config.
type Data struct {
	Datacenter   string `yaml:"datacenter"`
	ResourcePool string `yaml:"resource_pool"`
	Datastore    string `yaml:"datastore"`
	// StoragePolicy is the name of a vSphere Storage Policy (SPBM) to apply to the
	// cloned VM (home and disks). Optional; when empty the datastore default policy is used.
	StoragePolicy string `yaml:"storage_policy"`
	Network       string `yaml:"network"`
	Template      string `yaml:"template"` // VM template name to clone from (inventory)
	// ContentLibrary + LibraryItem select a vSphere Content Library OVF item to
	// deploy from, as an alternative to cloning an inventory Template. Optional;
	// mutually exclusive with Template. See issue #25.
	ContentLibrary string `yaml:"content_library"`
	LibraryItem    string `yaml:"library_item"`
	Folder         string `yaml:"folder"`  // VM folder path (optional)
	CACert         string `yaml:"ca_cert"` // PEM-encoded CA certificate (optional)
	// Tags lists vCenter tags to attach to the VM after cloning. Each entry is a
	// tag name, or "category/name" when the tag name is not unique across
	// categories. All tags (and categories) must already exist in vCenter.
	Tags     []string `yaml:"tags"`
	DiskSize uint64   `yaml:"disk_size"` // GiB
	CPU      uint     `yaml:"cpu"`
	Memory   uint     `yaml:"memory"` // MiB
	// ClusterFolder, when true, places the VM in a subfolder (under Folder, or the
	// datacenter VM folder) named after the cluster. The name is best-effort: it is
	// derived from the Omni machine request set ID by stripping the default
	// "-control-planes"/"-workers" machine set suffixes; custom-named machine sets
	// get a folder named after the full machine set ID. Missing folders are created.
	ClusterFolder bool `yaml:"cluster_folder"`
}

// Validate checks that the provider data selects exactly one VM source: an
// inventory Template, or a Content Library item (ContentLibrary + LibraryItem).
func (d Data) Validate() error {
	usingTemplate := d.Template != ""
	usingLibrary := d.ContentLibrary != "" || d.LibraryItem != ""

	switch {
	case usingTemplate && usingLibrary:
		return errors.New("both template and content_library/library_item are set; they are mutually exclusive")
	case !usingTemplate && !usingLibrary:
		return errors.New("no VM source set: specify either template, or content_library + library_item")
	case usingLibrary && (d.ContentLibrary == "" || d.LibraryItem == ""):
		return errors.New("content_library and library_item must both be set")
	}

	return nil
}
