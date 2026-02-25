## [Unreleased]

### Contributors

* Olli Hauer

### Added

- Optional folder parameter for VM placement
- Network configuration support for VM provisioning
- Disk resizing capability for VM provisioning
- UUID conversion utility to handle vSphere/Talos UUID format differences

### Fixed

- **Hostname persistence across reboots** - Convert vSphere UUIDs to Talos format to ensure config patches are correctly linked to machines
  - **Requirements**: Talos v1.12.0+ (multi-document configuration support)
  - **Tested with**: Talos v1.12.4 and Omni v1.5.7
- vSphere session keepalive with active SOAP handler
- Error handling and config validation improvements
- Hostname setting for created VMs

### Changed

- Config patch creation now happens after VM UUID is set to ensure proper linking

## [ 0.1.0-alpha.0](https://github.com///releases/tag/v0.1.0-alpha.0) (2025-11-05)

Welcome to the v0.1.0-alpha.0 release of !



Please try out the release binaries and report any issues at
https://github.com/siderolabs/omni-infra-provider-vsphere/issues

### Contributors

* Spencer Smith

### Changes
<details><summary>1 commit</summary>
<p>

* [`45ade1c`](https://github.com///commit/45ade1ca017bf37438ed07d793a5ca5204d92ef6) feat: initial commit of working vsphere provider
</p>
</details>

### Dependency Changes

This release has no dependency changes

