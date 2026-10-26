export declare class ExactJSONNumber {
    readonly literal: string;
    constructor(literal: string);
    toString(): string;
}
export declare const exactNumberText: (value: unknown) => string;
export declare function compareExactNumbers(a: unknown, b: unknown): number;
export declare const exactInteger: (value: unknown) => boolean;
export declare function parseExactJSON(text: string): any;
export declare function stringifyExactJSON(value: unknown): string;
export declare function parseV5Envelope(raw: string): any;
