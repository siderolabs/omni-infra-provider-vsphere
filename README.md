# Omni Infrastructure Provider for vSphere

Can be used to automatically provision Talos nodes in `vSphere`.

**NOTE: This provider assumes that you will be creating Talos nodes of v1.12.x or greater.**
**There is no support for older versions of Talos.**

## Running Infrastructure Provider

Create the configuration file for the provider:

```yaml
vsphere:
  uri: https://<vsphere IP or dns name>/sdk
  user: <vsphere user>
  password: <vsphere pass>
  insecureSkipVerify: true
```

### Using Docker

Copy the provider credentials created in omni to an `.env` file.

```env
# your omni instance URL
OMNI_ENDPOINT=https://<OMNI_INSTANCE_NAME>.<REGION>.omni.siderolabs.io
# base64 encoded key as shown by omni
OMNI_SERVICE_ACCOUNT_KEY=<PROVIDER_KEY>
```

Run in docker with:

```bash
docker run --name omni-infra-provider-vsphere --rm -it -e USER=user --env-file /tmp/omni-provider-vsphere.env -v /tmp/omni-provider-vsphere.yaml:/config.yaml ghcr.io/siderolabs/omni-infra-provider-vsphere --config-file /config.yaml
```

## Prerequisites to Use

Before using the vSphere provider to create machines, you will need to import an OVA as a template for the provider to clone from.
This should be generated from [https://factory.talos.dev](https://factory.talos.dev).
Select the VMWare image and add the vmtoolsd system extension (and any other desired extensions).
You **should not** add in kernel args to do joining to Omni.
These will be set by the provider in the talos.config guestinfo section when creating the VM.
In the future, the provider will may support seeding the environment with this image.

## Use

See [test/](./test/) for some examples, but generally:

- Create a machine class with `omnictl apply -f machineclass.yaml`
- Create a cluster that uses the machine class with `omnictl cluster template sync -f cluster-template.yaml`

### Machine class provider data

The `providerdata` block of the machine class accepts the following fields:

| Field | Required | Description |
| --- | --- | --- |
| `datacenter` | yes | vSphere datacenter name |
| `resource_pool` | yes | Resource pool to place the VM in |
| `datastore` | yes | Datastore to clone the VM onto |
| `network` | yes | Network to attach the VM's adapter to |
| `template` | yes\* | Name of the OVA template to clone from (*required if not using a Content Library). |
| `content_library` | yes\* | Content Library name (*required if `template` is not used). |
| `library_item` | yes\* | Name of the content library item (*required if `content_library` is used). |
| `cpu` | yes | Number of vCPUs |
| `memory` | yes | Memory in MB |
| `disk_size` | yes | Boot disk size in GB |
| `firmware` | no | Boot firmware for the VM, `bios` or `efi`; inherited from the template or OVF when omitted |
| `folder` | no | VM folder path |
| `cluster_folder` | no | When `true`, VMs are placed in a subfolder named after the cluster, created automatically under `folder` (or the datacenter VM folder) |
| `storage_policy` | no | Name of a vSphere Storage Policy (SPBM) applied to the VM home and disks; the datastore default policy is used when omitted |
| `ca_cert` | no | PEM-encoded CA certificate to add to the node's trusted roots |
| `tags` | no | List of vCenter tags to attach to the VM; each entry is a tag name, or `category/name` when the tag name is not unique across categories |
| `pci_devices` | no | List of physical PCI devices to pass through to the VM; each entry sets either `label` or `address` |
| `extra_config` | no | List of vSphere Advanced Parameters to set on the VM; each entry is a `key` and a `value` |

When `storage_policy` is set, the named policy is resolved against vCenter and applied to both the VM home and every disk during the clone.
This lets you, for example, give control plane (etcd) disks a Storage Policy with a higher Storage I/O Control share or reservation than worker disks.
Provisioning fails with a clear error if the named policy does not exist.

When `cluster_folder` is set to `true`, the provider creates (if needed) a VM folder for each cluster and clones the VMs into it.
The folder name is derived from the Omni machine set backing the request: the default `-control-planes` and `-workers` suffixes are stripped, so both machine sets of a cluster share one folder named after the cluster.
The exact cluster name is not available to infrastructure providers at provision time, so for custom-named worker machine sets the folder is named after the full machine set (e.g. `mycluster-storage-nodes`).
Folders are not removed on deprovision.

When `tags` is set, each named tag is resolved against vCenter and attached to the VM after cloning, before power-on.
This lets external tooling that keys off vCenter tags (for example backup software assigning VMs to backup policies) pick up the machines without manual tagging.
The tags and their categories must already exist in vCenter; the provider does not create them.
A plain tag name must be unique across categories — use the `category/name` form to disambiguate.
Provisioning fails with a clear error if a tag cannot be resolved.

### Boot firmware

`firmware` pins the VM's boot firmware to `bios` or `efi`.
When it is omitted, vSphere inherits the firmware of the template or OVF the VM was created from, which is how the provider has always behaved.

This matters because a Talos OVA downloaded from [factory.talos.dev](https://factory.talos.dev) imports as a **BIOS** VM: the OVF wrapper declares BIOS, so every VM cloned from that template boots BIOS as well.
Setting `firmware: efi` in the machine class produces UEFI VMs directly, instead of having to deploy the OVA once, change the firmware by hand, and convert it back into a template.

```yaml
      template: "talos-template"
      firmware: "efi"
```

Two things to know:

- The firmware is applied at provision time only, as a reconfigure of the new VM while it is still powered off, and it works the same for the template and Content Library paths.
  Changing `firmware` in a machine class does not touch VMs that already exist.
- Switching a template to `efi` only works if its disk image can boot UEFI.
  Talos images carry an EFI system partition, so this is safe for Talos templates; it is not a safe blanket change for arbitrary templates, where it shows up as a VM that no longer finds a boot device.

### PCI passthrough and advanced parameters

`pci_devices` attaches physical PCI devices — GPUs, SR-IOV NICs, FPGAs, NVMe drives — to the VM after it is created and before it is powered on.
Each entry selects a device in one of two ways, and exactly one of the two must be set:

- `label` uses **Dynamic DirectPath I/O** and matches the hardware label of a PCI device on an ESXi host.
  Prefer this: vCenter picks any host that has a device with that label, so the machine class is not tied to a single host.
  The label must be set on the host's PCI device beforehand (Host → Hardware → PCI Devices in vCenter); the provider does not create it.
- `address` uses **static DirectPath I/O** and takes a raw `bus:slot.function` address.
  This pins the VM to the one host holding that device, so provisioning fails if the VM lands elsewhere.
  The device must already be marked available for passthrough on that host.

Attaching any PCI device makes vSphere require a full memory reservation before the VM will power on, so the provider reserves all guest memory automatically whenever `pci_devices` is set.

`extra_config` sets arbitrary vSphere Advanced Parameters (VM Options → Advanced → Configuration Parameters).
Each entry is a `key` and a `value`, applied in the order written.
It is a list rather than a map so that the machine class form in the Omni UI can render it; a free-form map shows up there as a label with no editable field.
Values are strings exactly as vCenter stores them — `"TRUE"`, not `true`.
The provider owns `guestinfo.talos.config` and `disk.enableUUID`; setting either through `extra_config` is rejected, since overwriting the former would stop the machine from ever joining Omni.

Large-BAR GPUs such as the A100 and H100 will not initialize without 64-bit MMIO, which has no dedicated field — set it through `extra_config`:

```yaml
metadata:
  namespace: default
  type: MachineClasses.omni.sidero.dev
  id: vsphere-gpu-node
spec:
  autoprovision:
    providerid: vsphere
    providerdata: |
      cpu: 8
      memory: 32768 # in MB
      disk_size: 100 # in GB
      network: "VM Network"
      datastore: "datastore1"
      datacenter: "Datacenter"
      resource_pool: "gpu-pool"
      template: "talos-template"
      pci_devices:
        - label: "nvidia-a100"
      extra_config:
        - key: "pciPassthru.use64bitMMIO"
          value: "TRUE"
        - key: "pciPassthru.64bitMMIOSizeGB"
          value: "128"
```

Size `pciPassthru.64bitMMIOSizeGB` to comfortably exceed the total BAR size of the passed-through devices; too small a value shows up as a power-on failure rather than a configuration error.

## Development

See `make help` for general build info.

Build an image:

```shell
make generate image-omni-infra-provider-vsphere-linux-amd64
```
