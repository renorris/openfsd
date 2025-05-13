import $ from "jquery";
import jqXHR = JQuery.jqXHR;
import { decodeJwt } from "jose";

interface APIV1Response {
    version: string,
    err: string | null,
    data: any | null,
}

export async function doAPIRequest(method: string, url: string, withAuth: boolean, data: any): Promise<APIV1Response> {
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
        }).done((data) => {
            resolve(data)
        }).fail((data) => {
            resolve(data)
        });
    });
}

async function doAPIRequestWithoutBearerToken(method: string, url: string, data: any): Promise<jqXHR> {
    return $.ajax(url, {
        method: method,
        data: JSON.stringify(data),
    })
}

async function doAPIRequestWithBearerToken(method: string, url: string, data: any): Promise<jqXHR> {
    const accessToken = await getAccessToken();

    return $.ajax(url, {
        method: method,
        headers: {"Authorization": `Bearer ${accessToken}`},
        data: JSON.stringify(data),
    })
}

// getAccessToken returns the current valid access token.
// An exception is thrown if no token is found, an error occurs refreshing the access token,
// or if the refresh token is expired.
async function getAccessToken(): Promise<string> {
    const storedAccessToken = localStorage.getItem("access_token")!;
    const jwtPayload = decodeJwt(storedAccessToken);
    const exp = jwtPayload.exp!;
    const now = Math.floor(Date.now() / 1000);
    if (exp < (now + 15)) { // Assuming corrected logic
        const newAccessToken = await refreshAccessToken();
        localStorage.setItem("access_token", newAccessToken);
        return newAccessToken;
    }
    return storedAccessToken;
}

async function refreshAccessToken(): Promise<string> {
    const storedRefreshToken = localStorage.getItem("refresh_token")!;
    const jwtPayload = decodeJwt(storedRefreshToken);
    const exp = jwtPayload.exp!;
    const now = Math.floor(Date.now() / 1000);
    if (exp < (now + 15)) {
        throw new Error("refresh token expired");
    }

    return new Promise((resolve, reject) => {
        $.ajax({
            url: "/api/v1/auth/refresh",
            method: "POST",
            contentType: "application/json",
            data: JSON.stringify({ 'refresh_token': storedRefreshToken }),
        }).done((data) => resolve(data['access_token']))
            .fail(() => reject(new Error("failed to refresh access token")));
    });
}
