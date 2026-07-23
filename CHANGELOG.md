## [Omni Infra Provider vSphere 0.1.0-alpha.2](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/releases/tag/v0.1.0-alpha.2) (2026-07-23)

Welcome to the v0.1.0-alpha.2 release of Omni Infra Provider vSphere!  
*This is a pre-release of Omni Infra Provider vSphere*



Please try out the release binaries and report any issues at
https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/issues.

### linux/arm64 Provider Image

The provider container image is now also built for linux/arm64.


### Group VMs Into a Cluster Folder

A new `cluster_folder` option groups all VMs belonging to a cluster into a dedicated vSphere folder.


### Deploy From vSphere Content Library

VMs can now be provisioned from an OVF item in a vSphere Content Library, in addition to existing templates.


### Network Configuration and Disk Resizing

The provider now supports configuring VM networking and resizing disks during provisioning.


### Reliability Improvements

The vSphere session is now kept alive and automatically reconnected when it drops, and error handling and configuration validation have been improved throughout the provider.


### Root CA Injection

The provider can inject a custom root CA into provisioned machines. This requires Talos >= v1.12.


### Storage Policy Based Management (SPBM)

An optional vSphere storage policy can now be applied to provisioned VMs.


### Attach vCenter Tags

Provisioned VMs can now be tagged with vCenter tags, making it easier to organize and identify machines managed by the provider.


### Add Versioning Support

The infra provider will now report its version to Omni.


### VM Placement Folder

An optional `folder` parameter allows placing provisioned VMs into a specific vSphere folder.


### Contributors

* Golden Garlic
* Rowan Voermans
* Artem Chernyshev
* Edward Sammut Alessi
* Paul Verhoeven

### Changes
<details><summary>9 commits</summary>
<p>

* [`aee50c7`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/aee50c7e393d59c32fbbb770a147308a0a3de0f8) feat: rekres and add versioning
* [`2e68557`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/2e68557f360991efb1bb9ff991556863c073cb92) feat(provider): deploy from a vSphere Content Library OVF item
* [`8375b95`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/8375b959d1e30ae79ce35ec6a8adb10ae4938af7) feat(provider): support attaching vCenter tags to provisioned VMs
* [`ff340c4`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/ff340c44443c94d38acf0dcf3064b60a598d663c) fix(provider): resolve golangci-lint failures on cluster_folder change
* [`5354fcb`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/5354fcb4e01e359297cb5dca005caf3dd86d5ae3) feat(provider): add cluster_folder option to group VMs per cluster
* [`6280cd3`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/6280cd34e20581cb9886c42f7278e527bea6b413) feat(provider): apply optional vSphere storage policy (SPBM) to VMs
* [`eb50433`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/eb50433ae728420c595fd71b3711d41ef43603c2) feat(image): build linux/arm64 image variant
* [`fa88c81`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/fa88c81499373dadb9ee1039f7b63196ba3be407) chore: bump deps
* [`61177a3`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/61177a378e8e177bb1a8a81abd1df469c29dbae4) chore: add folder to example machineclass
</p>
</details>

### Dependency Changes

* **github.com/cosi-project/runtime**            v1.13.0 -> v1.16.2
* **github.com/siderolabs/omni/client**          v1.5.0 -> 582730ce940c
* **github.com/siderolabs/talos/pkg/machinery**  10f49ca91a61 -> v1.14.0-alpha.2
* **github.com/vmware/govmomi**                  v0.52.0 -> v0.53.0
* **go.uber.org/zap**                            v1.27.1 -> v1.28.0
* **go.yaml.in/yaml/v4**                         v4.0.0-rc.6 **_new_**

Previous release can be found at [v0.1.0-alpha.1](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/releases/tag/v0.1.0-alpha.1)

## [omni-infra-provider-vsphere 0.1.0-alpha.1](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/releases/tag/v0.1.0-alpha.1) (2026-03-06)

Welcome to the v0.1.0-alpha.1 release of omni-infra-provider-vsphere!  
*This is a pre-release of omni-infra-provider-vsphere*



Please try out the release binaries and report any issues at
https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/issues.

### Contributors

* Olli Hauer
* Spencer Smith
* Fritz Schaal
* Kevin Tijssen
* Paul Verhoeven

### Changes
<details><summary>12 commits</summary>
<p>

* [`382ebcb`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/382ebcbc27449263535fe4e5c795dab924aa61d3) chore: update machinery and use standard patching for CA certs
* [`bd32c4a`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/bd32c4af4199e1f2c134946cad3571b244f9f8f1) feat: support root CA injection, support only Talos >= v1.12
* [`be84955`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/be84955670128ea659d10fe7aaf7344e1a78206f) chore: rekres and fix config patch naming
* [`f004d94`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/f004d94e481a643c41b79c17100e29bf25255f64) chore: fix hostname setting for created VMs
* [`a0c3ffa`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/a0c3ffa866eec6b8fa6dc43150825c11d54cf77d) feat: add optional folder parameter for VM placement
* [`a336fb7`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/a336fb78115ba3ffeddfeaacd017f6d00aa639d9) fix: improve vSphere session keepalive with active SOAP handler
* [`e16143d`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/e16143dad22e34af412fb01bef9e07316db3c85f) fix: improve error handling and add config validation
* [`a8a039c`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/a8a039cb499e2e8802af509471e5486948c4a5fb) feat: implement network configuration for VM provisioning
* [`4070104`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/4070104306d301cfc4e9530a33baee7e0f081374) feat: implement disk resizing for VM provisioning
* [`36478f5`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/36478f5b935986def37189ea068a2eccbd78be2c) fix: add vSphere session keep-alive and reconnection logic
* [`66e937c`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/66e937c387062f0734b9e4a0a5547f85f2fd09f6) chore: fix typo in readme
* [`cd92dd7`](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/commit/cd92dd7c8678b0b8419b19e1b573b6a40e5e3ab2) fix: properly handle unset datacenter on node deprovision
</p>
</details>

### Dependency Changes

* **github.com/cosi-project/runtime**            v1.12.0 -> v1.13.0
* **github.com/planetscale/vtprotobuf**          79df5c4772f2 -> ba97887b0a25
* **github.com/siderolabs/omni/client**          3244ac4f41f5 -> v1.5.0
* **github.com/siderolabs/talos/pkg/machinery**  10f49ca91a61 **_new_**
* **github.com/spf13/cobra**                     v1.10.1 -> v1.10.2
* **go.uber.org/zap**                            v1.27.0 -> v1.27.1
* **google.golang.org/protobuf**                 v1.36.10 -> f2248ac996af

Previous release can be found at [v0.1.0-alpha.0](https://github.com/https://github.com/siderolabs/omni-infra-provider-vsphere/releases/tag/v0.1.0-alpha.0)

## [ 0.1.0-alpha.0](https://github.com///releases/tag/v0.1.0-alpha.0) (2025-11-05)

Welcome to the v0.1.0-alpha.0 release of !



Please try out the release binaries and report any issues at
https://github.com///issues.

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

