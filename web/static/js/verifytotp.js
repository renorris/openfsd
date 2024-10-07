async function handleVerifyTOTPSubmit(ev) {
    ev.preventDefault()

    const totp = $('#totp').val()

    const res = await fetch("/api/v1/auth/verifytotp", {
        body: JSON.stringify({
            code: parseInt(totp),
        }),
        cache: "no-cache",
        method: "POST",
        credentials: "include",
    })

    if (res.status !== 200) {
        const msg = res.status === 401 ? 'incorrect 2FA code' : `HTTP ${res.status} ${res.statusText}`
        setAlert('alert', `Error: ${msg}`, 'danger')
        return
    }

    const resPayload = await res.json()

    localStorage.setItem("token", resPayload.token)
    window.location.replace('/dashboard')
}

$('#form').submit(handleVerifyTOTPSubmit)