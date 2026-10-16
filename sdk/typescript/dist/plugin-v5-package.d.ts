import type { PluginV5Runtime } from "./plugin-v5-package-shape.js";
export interface PluginV5PublisherTrustKey {
    publisher: string;
    key_id: string;
    public_key: string;
    status: "active" | "revoked";
}
export interface PluginV5PublisherTrustStore {
    contract: "campusos.plugin-trust/v1";
    keys: PluginV5PublisherTrustKey[];
}
export interface PluginV5PackageCheckResult {
    api_version: "campusos.plugin/v5";
    publisher: string;
    key: string;
    version: string;
    runtime: PluginV5Runtime;
    package_digest: string;
    file_count: number;
    signature_verified: boolean;
    installed: false;
}
export type PluginV5PackageErrorCode = "plugin.package_invalid" | "plugin.package_digest_mismatch" | "plugin.package_signature_invalid" | "plugin.release_digest_conflict" | "plugin.legacy_protocol_removed";
