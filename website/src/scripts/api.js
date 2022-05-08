async function apiPost(url, body) {
    const response = await fetch(url, { method: 'POST', body });

    const status = response.status;

    const value = await response.json();
    const error = value?.err;

    if (error == null) {
        return { status, error: null, value };
    } else {
        return { status, error: String(error), value: null };
    }
}

export function apiUpload(body) {
    return apiPost('/api/upload', body);
}

export function apiSpin(token, options) {
    const url = '/api/spin?' + new URLSearchParams([['file', String(token.file)]]);
    const body = new Blob([JSON.stringify(options)], { type: 'application/json' });
    return apiPost(url, body);
}
