# Omni Infrastructure Provider for vSphere Helm Chart

![Version: 0.1.0-alpha.3](https://img.shields.io/badge/Version-0.1.0--alpha.3-informational?style=flat) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat) ![AppVersion: v0.1.0-alpha.3](https://img.shields.io/badge/AppVersion-v0.1.0--alpha.3-informational?style=flat)

A Helm chart to deploy the [Omni](https://omni.siderolabs.com) infrastructure provider for vSphere on a Kubernetes cluster.

The provider connects to an Omni instance as an infrastructure provider and clones Talos VMs in vSphere on demand for the clusters that use its machine classes.
It runs as a single pod that only makes outbound connections, to the Omni API and to vCenter, so the chart creates no Service or Ingress.

For all available configuration options, see the [`values.yaml`](values.yaml) file and the [Values](#values) section below.
For the provider itself, the vSphere prerequisites and the machine class fields, see the [provider README](../../../README.md).

**Homepage:** <https://www.siderolabs.com/omni/>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Sidero Labs | <info@siderolabs.com> | <https://www.siderolabs.com> |

## Installation

### 1. Register the provider in Omni

Create the infrastructure provider in Omni and copy the service account key it prints.
The key is shown only once.

```bash
omnictl infraprovider create vsphere
```

The ID used here (`vsphere`) must match `provider.id` in the chart values and the `providerid` of the machine classes that use the provider.

### 2. Create a values.yaml file

Create a `values.yaml` file with your configuration. See the example below.

### 3. Install the Chart

The chart version follows the provider release it packages.

```bash
helm install omni-infra-provider-vsphere oci://ghcr.io/siderolabs/charts/omni-infra-provider-vsphere --version 0.1.0-alpha.3 -n omni --create-namespace -f values.yaml
```

To upgrade an existing installation:

```bash
helm upgrade omni-infra-provider-vsphere oci://ghcr.io/siderolabs/charts/omni-infra-provider-vsphere --version 0.1.0-alpha.3 -n omni -f values.yaml
```

The provider registers itself in Omni on start.
Once it shows up as connected under **Infrastructure Providers** in the Omni UI, create machine classes with `providerid` set to `provider.id` and use them in cluster templates.

## Example

The chart needs three things: the URL of the Omni API, the service account key printed by `omnictl infraprovider create`, and the vCenter connection settings.
The vCenter settings are rendered into the provider config file and stored in a Secret managed by the chart.

```yaml
provider:
  # Must match the ID passed to `omnictl infraprovider create`
  id: vsphere

omni:
  # The advertised URL of the Omni API
  endpoint: https://omni.example.com
  serviceAccountKey:
    # Paste the key printed by `omnictl infraprovider create` here
    value: <your-omni-service-account-key>

config:
  vsphere:
    uri: https://vcenter.example.com/sdk
    user: omni@vsphere.local
    password: <your-vsphere-password>
```

## Using existing Secrets

Both credentials can be read from Secrets that are managed outside the chart, for example by the [External Secrets Operator](https://external-secrets.io/).
When an existing Secret is used, the chart does not create its own Secret for that credential.

```yaml
omni:
  endpoint: https://omni.example.com
  serviceAccountKey:
    existingSecret: omni-infra-provider-vsphere
    # Key inside the Secret, defaults to OMNI_SERVICE_ACCOUNT_KEY
    existingSecretKey: OMNI_SERVICE_ACCOUNT_KEY

# The Secret must hold the complete provider config file
existingConfigSecret: omni-infra-provider-vsphere-config
# Key inside the Secret, defaults to config.yaml
existingConfigSecretKey: config.yaml
```

The config file in `existingConfigSecret` has the same format as the `config` section of the values:

```yaml
vsphere:
  uri: https://vcenter.example.com/sdk
  user: omni@vsphere.local
  password: <your-vsphere-password>
  insecureSkipVerify: false
```

## Trusting a private vCenter CA

The provider image ships the Mozilla CA bundle.
If vCenter uses a certificate from a private CA, add that CA to the container trust store instead of setting `config.vsphere.insecureSkipVerify`:

```yaml
extraObjects:
  - apiVersion: v1
    kind: ConfigMap
    metadata:
      name: vcenter-ca
    data:
      vcenter-ca.crt: |
        -----BEGIN CERTIFICATE-----
        <your-ca-certificate>
        -----END CERTIFICATE-----

extraVolumes:
  - name: vcenter-ca
    configMap:
      name: vcenter-ca

extraVolumeMounts:
  - name: vcenter-ca
    mountPath: /etc/ssl/certs/vcenter-ca.crt
    subPath: vcenter-ca.crt
    readOnly: true
```

The same approach works for an Omni instance with a private CA, as an alternative to `omni.insecureSkipVerify`.

## Notes

- The provider always runs as a single replica.
  Every instance registered under the same provider ID would act on the same provision requests, so the Deployment uses the `Recreate` strategy and does not expose a replica count.
- The provider exposes no ports, so the chart configures no probes by default.
  Its state lives in Omni; a restarted provider picks up where it left off.
- The container runs unprivileged with a read-only root filesystem.
  Use `securityContext` to relax this if your cluster requires it.

## Values

Here are the configurable parameters of the chart and their default values.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Pod affinity settings for the provider pod |
| args | list | `[]` | Extra arguments to pass to the provider. Note: --config-file, --omni-api-endpoint, --id, --provider-name, --provider-description and --insecure-skip-verify are generated by the chart from the values below. |
| config.vsphere.insecureSkipVerify | bool | `false` | Skip TLS certificate verification for vCenter. Prefer adding the vCenter CA certificate to the container trust store with `extraVolumes`/`extraVolumeMounts` instead. |
| config.vsphere.password | string | `""` | Password of the vSphere user. Tip: use `existingConfigSecret` to load the whole config file from a Secret managed outside the chart. |
| config.vsphere.uri | string | `"https://vcenter.example.com/sdk"` | URL of the vCenter SDK endpoint. |
| config.vsphere.user | string | `""` | vSphere user the provider logs in with. |
| dnsConfig | object | `{}` | DNS configuration for the provider pod. |
| dnsPolicy | string | `""` | DNS policy for the provider pod. |
| env | list | `[]` | Environment variables to pass to the provider. |
| envFrom | list | `[]` | envFrom to pass to the provider. |
| existingConfigSecret | string | `""` | Name of an existing Secret that holds the provider config file. If set, the `config` section is ignored and the chart does not create a config Secret. This is useful for bringing in the vSphere credentials from an external source (e.g., External Secrets Operator). |
| existingConfigSecretKey | string | `"config.yaml"` | Key inside `existingConfigSecret` that holds the provider config file. |
| extraContainers | list | `[]` | Extra containers (sidecars) to add to the provider pod. |
| extraObjects | list | `[]` | Extra Kubernetes objects to deploy with the helm chart |
| extraVolumeMounts | list | `[]` | List of additional mounts to add (normally used with extraVolumes) |
| extraVolumes | list | `[]` | List of extra volumes to add |
| fullnameOverride | string | `""` | String to fully override `"omni-infra-provider-vsphere.fullname"`. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy for the provider. |
| image.repository | string | `"ghcr.io/siderolabs/omni-infra-provider-vsphere"` | Repository to use for the provider. |
| image.tag | string | `.Chart.AppVersion` | Tag to use for the provider. |
| imagePullSecrets | list | `[]` | Secrets with credentials to pull images from a private registry. |
| initContainers | list | `[]` | Init containers to add to the provider pod. |
| livenessProbe | object | `{}` | Liveness probe for the provider container. The provider exposes no HTTP endpoint, so no probe is configured by default. |
| nameOverride | string | `""` | Provide a name in place of `"omni-infra-provider-vsphere"`. |
| namespaceOverride | string | `""` | String to fully override `"omni-infra-provider-vsphere.namespace"`. |
| nodeSelector | object | `{}` | Node selector to constrain which nodes the provider pod is scheduled on |
| omni.endpoint | string | `"https://omni.example.com"` | URL of the Omni API the provider connects to. MUST be specified as a full URL, including scheme (https://). This is the advertised URL of the Omni API (`config.services.api.advertisedURL` in the Omni chart). |
| omni.insecureSkipVerify | bool | `false` | Skip TLS certificate verification for the Omni API. |
| omni.serviceAccountKey.existingSecret | string | `""` | Name of an existing Secret that holds the service account key. If set, `value` is ignored and the chart does not create a Secret. |
| omni.serviceAccountKey.existingSecretKey | string | `"OMNI_SERVICE_ACCOUNT_KEY"` | Key inside the existing Secret that holds the service account key. |
| omni.serviceAccountKey.value | string | `""` | The service account key, as shown by Omni. It is stored in a Secret managed by the chart. |
| podAnnotations | object | `{}` | Additional annotations to add to the provider pod |
| podLabels | object | `{}` | Additional labels to add to the provider pod |
| podSecurityContext | object | `{}` | Pod-level security context |
| priorityClassName | string | `""` | Priority class name for the provider pod. |
| provider.description | string | `"vsphere infrastructure provider"` | Provider description as it appears in Omni. |
| provider.id | string | `"vsphere"` | ID of the infrastructure provider. It must match the ID the provider was created with in Omni (`omnictl infraprovider create <id>`) and the `providerid` of the machine classes that use it. |
| provider.name | string | `"vsphere"` | Provider name as it appears in Omni. |
| readinessProbe | object | `{}` | Readiness probe for the provider container. |
| resources | object | `{}` | CPU and Memory resource requests and limits. |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsGroup":65534,"runAsNonRoot":true,"runAsUser":65534,"seccompProfile":{"type":"RuntimeDefault"}}` | Container-level security context. The provider is a static binary that only makes outbound connections, so it runs unprivileged on a read-only root filesystem. |
| serviceAccount.annotations | object | `{}` | Annotations for the ServiceAccount |
| serviceAccount.automount | bool | `false` | Automount API credentials for a ServiceAccount. The provider does not use the Kubernetes API. |
| serviceAccount.create | bool | `true` | Whether to create a ServiceAccount for the provider |
| serviceAccount.name | string | `""` | Name of the ServiceAccount to use. If not set and create is true, a name is generated using the fullname template. |
| strategy.type | string | `"Recreate"` | Deployment strategy type. |
| terminationGracePeriodSeconds | int | `30` | Termination grace period in seconds for the provider pod. |
| tolerations | list | `[]` | Tolerations for the provider pod |
