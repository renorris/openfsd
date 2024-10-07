function showModal(id) {
    const m = new bootstrap.Modal(document.getElementById(id))
    m.show()
}

function hideModal(id) {
    const m = new bootstrap.Modal(document.getElementById(id))
    m.hide()
}