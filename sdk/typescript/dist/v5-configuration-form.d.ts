import { ExactJSONNumber } from "./exact-json";
export type ConfigValue = string | boolean | number | ExactJSONNumber;
export interface ConfigField {
    type: "string" | "boolean" | "integer" | "number";
    title: string;
    enum?: ConfigValue[];
    minimum?: ConfigValue;
    maximum?: ConfigValue;
    minLength?: number;
    maxLength?: number;
    selector?: "profile" | "public_collection";
}
export interface Coordinate {
    publisher: string;
    plugin_key: string;
    version: string;
}
export interface ConfigEntry {
    release: Coordinate & {
        package_digest?: string;
        manifest: {
            api_version: string;
            display_name?: string;
            runtime?: string;
            consumes?: any[];
            host_resources?: any;
            data: {
                config_scope: string;
            };
            configuration: unknown;
        };
    };
    installation?: {
        installed: boolean;
        observed: string;
        desired: string;
        error_code: string;
        generation?: string;
        version?: string;
        desired_version?: string;
    };
    configuration?: {
        owner_user_id: string;
        revision: string;
        values: Record<string, unknown>;
        secret_refs: Record<string, string>;
    };
    grant?: any;
    consent?: any;
    purpose_hash?: string;
}
export interface ConfigOptions {
    profiles: Array<{
        id: string;
        title: string;
    }>;
    public_collections: Array<{
        id: string;
        title: string;
    }>;
    secret_refs: Record<string, Array<{
        id: string;
        title: string;
    }>>;
}
export interface ConfigForm {
    fields: Record<string, ConfigField>;
    required: string[];
    values: Record<string, ConfigValue>;
    secretNames: string[];
    secretRefs: Record<string, string>;
    revision: string;
    options: ConfigOptions;
}
export declare const emptyConfigOptions: () => ConfigOptions;
export declare const coordinateKey: (c: Coordinate) => string;
export declare function normalizeValues(form: ConfigForm, values: Record<string, unknown>): Record<string, ConfigValue>;
export declare function validateValues(form: ConfigForm, values: Record<string, unknown>): string;
export declare function configurationForm(entry: ConfigEntry, options?: ConfigOptions, scope?: "system" | "user"): ConfigForm;
