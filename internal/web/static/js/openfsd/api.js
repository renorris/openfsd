// First-party API helper: cookie session + CSRF (no localStorage JWT, no jQuery).

function getCSRFToken() {
    const meta = document.querySelector('meta[name="csrf-token"]');
    if (meta && meta.content) {
        return meta.content;
    }
    // Double-submit cookie fallback
    const match = document.cookie.match(/(?:^|;\s*)openfsd_csrf=([^;]+)/);
    return match ? decodeURIComponent(match[1]) : "";
}

async function doAPIRequestWithAuth(method, url, data) {
    return doAPIRequest(method, url, true, data);
}

async function doAPIRequestNoAuth(method, url, data) {
    return doAPIRequest(method, url, false, data);
}

/**
 * Perform an API request. When withAuth is true, sends same-origin cookies
 * (session) and CSRF on mutations. Bearer from localStorage is no longer used
 * for first-party UI (KD-18 dual-accept still allows Bearer for external tools).
 */
async function doAPIRequest(method, url, withAuth, data) {
    const headers = {
        "Accept": "application/json",
    };
    const upper = (method || "GET").toUpperCase();
    const hasBody = data !== undefined && data !== null && upper !== "GET" && upper !== "HEAD";
    if (hasBody) {
        headers["Content-Type"] = "application/json";
    }
    if (withAuth && upper !== "GET" && upper !== "HEAD") {
        const csrf = getCSRFToken();
        if (csrf) {
            headers["X-CSRF-Token"] = csrf;
        }
    }

    const opts = {
        method: upper,
        headers: headers,
        credentials: "same-origin",
    };
    if (hasBody) {
        opts.body = JSON.stringify(data);
    }

    const res = await fetch(url, opts);
    let body = null;
    const ct = res.headers.get("content-type") || "";
    if (ct.includes("application/json")) {
        try {
            body = await res.json();
        } catch (_) {
            body = null;
        }
    }

    if (!res.ok) {
        const err = new Error((body && body.err) || res.statusText || "request failed");
        err.status = res.status;
        err.responseJSON = body;
        throw err;
    }
    return body;
}

function logout() {
    // Prefer the no-JS form POST in the layout nav; this is a fallback.
    const form = document.createElement("form");
    form.method = "post";
    form.action = "/logout";
    const csrf = getCSRFToken();
    if (csrf) {
        const input = document.createElement("input");
        input.type = "hidden";
        input.name = "csrf_token";
        input.value = csrf;
        form.appendChild(input);
    }
    document.body.appendChild(form);
    form.submit();
}
