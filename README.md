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

See [examples/](./examples/) for some examples, but generally:

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
| `cpu` | yes | Number of vCPUs |
| `memory` | yes | Memory in MB |
| `disk_size` | yes | Boot disk size in GB |
| `template` | yes* | Name of the OVA template to clone from (*required if not using a Content Library). |
| `content_library` | yes* | Content Library name (*required if `template` is not used). |
| `library_item` | yes* | Name of the content library item (*required if `content_library` is used). |
| `folder` | no | VM folder path |
| `cluster_folder` | no | When `true`, VMs are placed in a subfolder named after the cluster, created automatically under `folder` (or the datacenter VM folder) |
| `storage_policy` | no | Name of a vSphere Storage Policy (SPBM) applied to the VM home and disks; the datastore default policy is used when omitted |
| `ca_cert` | no | PEM-encoded CA certificate to add to the node's trusted roots |
| `tags` | no | List of vCenter tags to attach to the VM; each entry is a tag name, or `category/name` when the tag name is not unique across categories |

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

## Development

See `make help` for general build info.

Build an image:

```shell
make generate image-omni-infra-provider-vsphere-linux-amd64
```
