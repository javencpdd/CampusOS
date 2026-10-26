import type { PluginV5Consume } from "./plugin-v5-consumes.js";
import type { PluginV5Manifest } from "./plugin-v5-manifest.js";

/** Host facts only. A DTO is never a proof, resource ACL or dispatch permit. */
export interface PluginV5Coordinate {
  publisher: string;
  plugin_key: string;
  version: string;
}
export interface PluginV5Installation {
  publisher: string;
  plugin_key: string;
  owner_user_id: string;
  version: string;
  desired_version: string;
  installed: boolean;
  desired: "enabled" | "disabled" | "uninstalled";
  observed: "pending" | "ready" | "failed" | "stopped";
  generation: string;
  revision: string;
  observed_generation: string;
  active_generation: string;
  error_code: string;
  config_digest: string;
}
export interface PluginV5Operation extends PluginV5Coordinate {
  id: string;
  owner_user_id: string;
  kind: string;
  status: "queued" | "working" | "waiting" | "done" | "failed" | "superseded";
  generation: string;
  error_code: string;
  purge_data: boolean;
  attempts: number;
  revision: string;
  reason?: string;
}
type Request = { version: string; idempotency_key: string };
type ConfigurationPayload =
  | {
      configuration_part?: never;
      values: Record<string, unknown>;
      secret_refs?: Record<string, string>;
    }
  | {
      configuration_part: "values";
      values: Record<string, unknown>;
      secret_refs?: never;
    }
  | {
      configuration_part: "secret_refs";
      secret_refs: Record<string, string>;
      values?: never;
    };
export type PluginV5InstallationCommand = Request &
  (
    | {
        kind: "install" | "enable" | "disable" | "upgrade";
        expected_generation: string;
      }
    | { kind: "uninstall"; expected_generation: string; purge_data?: boolean }
    | ({ kind: "configure"; expected_revision: string } & ConfigurationPayload)
    | {
        kind: "consent";
        expected_revision: string;
        accepted: boolean;
        purpose_hash: string;
      }
    | { kind: "retry"; operation_id: string; expected_revision: string }
  );
export type PluginV5AdminInstallationCommand =
  | Exclude<PluginV5InstallationCommand, { kind: "consent" }>
  | (Request &
      (
        | {
            kind: "grant";
            expected_revision: string;
            approved: boolean;
            consumes: PluginV5Consume[];
            host_resources?: NonNullable<PluginV5Manifest["host_resources"]>;
            reason?: string;
            expires_at_ms?: string;
          }
        | { kind: "revoke"; expected_revision: string; reason?: string }
        | { kind: "purge_release" }
      ));
export type PluginV5InstallationErrorCode =
  | "plugin.platform_unavailable"
  | "plugin.not_found"
  | "plugin.permission_denied"
  | "plugin.lifecycle_generation_conflict"
  | "plugin.lifecycle_invalid"
  | "plugin.lifecycle_rollback_unverifiable"
  | "plugin.legacy_retired";
