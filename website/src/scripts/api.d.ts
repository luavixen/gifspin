export interface ApiOptions {
    width: number;
    height: number;

    frameCount: number;
    frameDelay: number;

    flagCrop: boolean;
    flagReverse: boolean;
    flagFlatten: boolean;

    background: number;
}

export interface ApiLimits {
    sizeMax: number;
    widthMax: number;
    heightMax: number;

    frameCountMin: number;
    frameCountMax: number;
    frameDelayMin: number;
    frameDelayMax: number;
}

export interface ApiToken {
    file: string;
}

export type ApiResponse<T> =
    | { status: number; error: null; value: T }
    | { status: number; error: string; value: null };

export declare function apiUpload(body: Blob): Promise<ApiResponse<ApiToken>>;

export declare function apiSpin(token: ApiToken, options: ApiOptions): Promise<ApiResponse<ApiToken>>;
