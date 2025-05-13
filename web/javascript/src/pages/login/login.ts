import $ from 'jquery';

import { doAPIRequest } from "../../api/api";

$("#login-form").on('submit', async (ev) => {
    ev.preventDefault()

    const requestBody = {
        'cid': $("#login-input-cid").val() as string,
        'password': $("#login-input-password").val(),
        'remember_me': $("#login-input-remember-me").prop('checked')
    }

    const res = await doAPIRequest('POST', '/api/v1/auth/login', false, requestBody);
    if (res.err !== null) {
        alert(`Error signing in: ${res.err}`);
        return
    }

    localStorage.setItem("access_token", res.data['access_token'])
    localStorage.setItem("refresh_token", res.data['refresh_token'])
})
