async function doAPIRequestWithAuth(method, url, data) {
    return doAPIRequest(method, url, true, data)
}

async function doAPIRequestNoAuth(method, url, data) {
    return doAPIRequest(method, url, false, data)
}

async function doAPIRequest(method, url, withAuth, data) {
    return new Promise(async (resolve, reject) => {
        let accessToken = "";
        if (withAuth) {
            accessToken = await getAccessToken();
        }

        $.ajax({
            url: url,
            method: method,
            headers: withAuth ? {"Authorization": `Bearer ${accessToken}`} : {},
            contentType: "application/json",
            data: JSON.stringify(data),
            dataType: "json",
        }).done((res) => {
            resolve(res)
        }).fail((xhr) => {
            reject(xhr)
        });
    });
}

// getAccessToken returns the current valid access token.
// An exception is thrown if no token is found, an error occurs refreshing the access token,
// or if the refresh token is expired.
async function getAccessToken() {
    const storedAccessToken = localStorage.getItem("access_token");

    if (!storedAccessToken) {
        window.location.href = "/login"
        throw new Error("No access token found")
    }

    const jwtPayload = decodeJwt(storedAccessToken);
    const exp = jwtPayload.exp;
    const now = Math.floor(Date.now() / 1000);
    if (exp < (now + 15)) { // Assuming corrected logic
        const newAccessToken = await refreshAccessToken();
        localStorage.setItem("access_token", newAccessToken);
        return newAccessToken;
    }
    return storedAccessToken;
}

async function refreshAccessToken() {
    const storedRefreshToken = localStorage.getItem("refresh_token");
    const jwtPayload = decodeJwt(storedRefreshToken);
    const exp = jwtPayload.exp;
    const now = Math.floor(Date.now() / 1000);
    if (exp < (now + 15)) {
        window.location.href = "/login";
        throw new Error("refresh token expired");
    }

    return new Promise((resolve, reject) => {
        $.ajax({
            url: "/api/v1/auth/refresh",
            method: "POST",
            contentType: "application/json",
            data: JSON.stringify({ 'refresh_token': storedRefreshToken }),
            dataType: "json",
        }).done((res) =>  {
            resolve(res.data.access_token)
        }).fail((xhr) => {
            window.location.href = "/login";
            reject(new Error("failed to refresh access token"))
        })
    });
}

function getAccessTokenClaims() {
    return decodeJwt(localStorage.getItem("access_token"))
}

function decodeJwt(token) {
    if (!token) {
        window.location.href = "/login";
        throw new Error("no token found")
    }

    var base64Url = token.split('.')[1];
    var base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
    var jsonPayload = decodeURIComponent(window.atob(base64).split('').map(function(c) {
        return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
    }).join(''));

    return JSON.parse(jsonPayload);
}

function logout() {
    localStorage.removeItem("access_token")
    localStorage.removeItem("refresh_token")
    window.location.href = "/login"
}
