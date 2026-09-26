export interface PluginV5DispatchCall {
    request_id: string;
    contract: "campusos.plugin-dispatch/v1";
    target: {
        kind: "host_api" | "contribution";
        id: string;
    };
    instance_generation: number;
    deadline_ms: number;
    request_bytes: number;
    response_limit_bytes: number;
}
export interface PluginV5DispatchTrustedFacts {
    now_ms: number;
    current_generation: number;
    desired_state: "enabled" | "disabled" | "uninstalled";
    observed_state: "ready" | "pending" | "failed" | "stopped";
    global_installed: boolean;
    user_install_required: boolean;
    user_installed: boolean;
    grant_active: boolean;
    consent_required: boolean;
    consent_active: boolean;
    declared: boolean;
    published: boolean;
    feature_enabled: boolean;
    instance_audience: "host-api" | "other";
    credential_expires_at_ms: number;
    selected_id: string;
    max_request_bytes: number;
    max_response_bytes: number;
    max_deadline_ms: number;
}
export interface PluginV5DispatchDecisionInput {
    direction: "host_to_plugin" | "plugin_to_host";
    call: PluginV5DispatchCall;
    trusted: PluginV5DispatchTrustedFacts;
}
export type PluginV5DispatchErrorCode = "plugin.call_invalid" | "plugin.call_target_mismatch" | "plugin.instance_stale" | "plugin.call_deadline" | "plugin.call_budget" | "plugin.runtime_unavailable" | "plugin.global_install_required" | "plugin.user_install_required" | "plugin.grant_revoked" | "plugin.consent_required" | "plugin.target_unpublished" | "plugin.target_undeclared" | "plugin.feature_disabled" | "plugin.instance_credential_invalid" | "plugin.contribution_selection_conflict";
