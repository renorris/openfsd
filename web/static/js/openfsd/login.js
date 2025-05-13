$("#login-form").on("submit", (ev) => {
    ev.preventDefault();

    const requestBody = {
        'cid': parseInt($("#login-input-cid").val()),
        'password': $("#login-input-password").val(),
        'remember_me': $("#login-form-remember-me").prop('checked')
    }

    $.ajax("/api/v1/auth/login", {
        method: "POST",
        contentType: "application/json",
        dataType: "json",
        data: JSON.stringify(requestBody)
    }).then((res) => {
        console.log(res);
        localStorage.setItem("access_token", res.data.access_token)
        localStorage.setItem("refresh_token", res.data.refresh_token)
        window.location.href = "/dashboard"
    }).fail((xhr) => {
        alert(`${xhr.statusText}: ${xhr.responseJSON['err']}`)
    })
})
