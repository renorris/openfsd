function setAlert(id, message, color) {
    const html = `
    <div class="alert alert-${color} alert-dismissible fade show" role="alert">
        <span>${message}</span>
        <button type="button" class="btn-close" data-bs-dismiss="alert" aria-label="Close"></button>
    </div>`

    $('#' + id).html(html)
}

function clearAlert(id) {
    $('#' + id).html('')
}