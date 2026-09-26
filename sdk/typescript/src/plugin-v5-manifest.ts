// G1 target wire Manifest. Runtime parsing and package signature checks belong to 03a/03b.
import type { PluginV5UiArtifact, PluginV5BackendArtifact } from "./plugin-v5-package-shape.js";
import type { PluginV5Consume } from "./plugin-v5-consumes.js";
import type { PluginV5Provide } from "./plugin-v5-provides.js";
import type { PluginV5ResourceSet } from "./plugin-v5-resource-install.js";
import type { PluginV5ConfigDefinition } from "./plugin-v5-config.js";
export interface PluginV5PrivateDataDeclaration {
  config_scope: "system" | "user" | "both";
  private_kv_bytes: number;
  private_object_bytes: number;
  upgrade: {
    schema_version: number;
    mode: "preserve" | "migrate" | "reset";
    rollback: "snapshot" | "forbidden";
  };
}
export interface PluginV5ManifestBase {
  api_version: "campusos.plugin/v5";
  publisher: string;
  key: string;
  version: string;
  host_contracts: string[];
  consumes: PluginV5Consume[];
  provides: PluginV5Provide[];
  configuration: PluginV5ConfigDefinition;
  data: PluginV5PrivateDataDeclaration;
}
export type PluginV5Manifest = PluginV5ManifestBase & (
  | { runtime: "none"; ui: PluginV5UiArtifact[]; backend?: never; host_resources?: never }
  | { runtime: "wasm"; backend: { artifact: Extract<PluginV5BackendArtifact, { kind: "wasm" }>; transport: "host-call" | "http" | "grpc" }; ui?: PluginV5UiArtifact[]; host_resources: PluginV5ResourceSet }
  | { runtime: "container"; backend: { artifact: Extract<PluginV5BackendArtifact, { kind: "container" }>; transport: "host-call" | "http" | "grpc" }; ui?: PluginV5UiArtifact[]; host_resources: PluginV5ResourceSet }
);
export type PluginV5ManifestErrorCode =
  | "plugin.manifest_invalid" | "plugin.manifest_shape_mismatch"
  | "plugin.manifest_capability_unpublished" | "plugin.manifest_extension_unpublished"
  | "plugin.manifest_schema_mismatch" | "plugin.manifest_resource_invalid"
  | "plugin.manifest_upgrade_unrecoverable" | "plugin.manifest_release_mismatch";
