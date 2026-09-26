// G1 target projection. Full manifest, signature verification and installation belong to later slices.
export const PLUGIN_V5_API_VERSION = "campusos.plugin/v5" as const;
export type PluginV5Runtime = "none" | "wasm" | "container";
export type PluginV5UiAudience = "user" | "admin";
export type PluginV5ExtensionPoint = "preview.renderer" | "ai.chat" | "knowledge.source";
export interface PluginV5UiArtifact {
  audience: PluginV5UiAudience;
  entrypoint: string;
  digest: string;
}
export type PluginV5BackendArtifact =
  | { kind: "wasm"; path: string; digest: string }
  | { kind: "container"; image: string };
export interface PluginV5ProviderShape {
  id: string;
  point: PluginV5ExtensionPoint;
  contract_version: "v1";
}
export interface PluginV5ShapeBase {
  api_version: typeof PLUGIN_V5_API_VERSION;
  publisher: string;
  key: string;
  version: string;
  host_contracts: string[];
  provides: PluginV5ProviderShape[];
}
export type PluginV5PackageShape = PluginV5ShapeBase & (
  | { runtime: "none"; ui: PluginV5UiArtifact[]; backend?: never }
  | { runtime: "wasm"; backend: { artifact: Extract<PluginV5BackendArtifact, { kind: "wasm" }>; transport: "host-call" | "http" | "grpc" }; ui?: PluginV5UiArtifact[] }
  | { runtime: "container"; backend: { artifact: Extract<PluginV5BackendArtifact, { kind: "container" }>; transport: "host-call" | "http" | "grpc" }; ui?: PluginV5UiArtifact[] }
);
export interface PluginV5ReleaseEnvelope {
  publisher: string;
  key: string;
  version: string;
  package_digest: string;
  signature: { key_id: string; value: string };
}
export type PluginV5ShapeErrorCode =
  | "plugin.shape_invalid" | "plugin.host_contract_unsupported"
  | "plugin.artifact_mismatch" | "plugin.contribution_empty"
  | "plugin.duplicate_contribution" | "plugin.release_identity_mismatch"
  | "plugin.release_digest_conflict";
