import type { ConfigEntry } from "./v5-configuration-form";
export declare function pluginDisplayName(entry: ConfigEntry): string;
export declare function capabilityTitle(code: string): string;
export declare function savedCapability(entry: ConfigEntry, consume: any): boolean;
export declare function approvedCapabilities(entry: ConfigEntry): any[];
export declare function consentRequired(entry: ConfigEntry): boolean;
export declare function consentCurrent(entry: ConfigEntry): boolean;
export declare function requiresSelectedResource(entry: ConfigEntry): boolean;
export declare function userProgress(entry: ConfigEntry, now?: number): {
    stage: string;
    label: string;
    detail: string;
    action: string;
};
export declare function sameConsumption(a: any, b: any): boolean;
export declare function approvalState(entry: ConfigEntry, now?: number): {
    ready: boolean;
    label: string;
};
export declare function enableBlockReason(entry: ConfigEntry, now?: number): string;
export declare function runtimeLabel(entry: ConfigEntry): string;
export declare function managementUIState(entry: ConfigEntry, now?: number): {
    declared: boolean;
    canOpen: boolean;
    message: string;
};
