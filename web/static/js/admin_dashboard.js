function getCookie(name) {
    const value = `; ${document.cookie}`;
    const parts = value.split(`; ${name}=`);
    if (parts.length === 2) return parts.pop().split(';').shift();
}

const showResponse = (msg) => {
    $('#responseBody').text(msg);
    const toast = new bootstrap.Toast($('#responseToast'));
    toast.show();
};

$(document).ready(() => {
    const token = getCookie("token")

    const userFetchModal = $("#user-fetch-modal")
    const userEditModal = $("#user-edit-modal")
    const userCreateModal = $("#user-create-modal")

    const userCreateStatusMsg = $("#user-create-statusmsg")

    const userEditCID = $("#user-edit-cid-header")
    const userEditEmail = $("#user-edit-email")
    const userEditFirstname = $("#user-edit-firstname")
    const userEditLastname = $("#user-edit-lastname")
    const userEditNetworkRating = $("#user-edit-networkrating")
    const userEditPilotRating = $("#user-edit-pilotrating")
    const userEditLastUpdated = $("#user-edit-lastupdated")
    const userEditCreatedAt = $("#user-edit-createdat")

    const userEditPassword = $("#user-edit-password")
    const userEditFSDPassword = $("#user-edit-fsdpassword")

    const userEditUpdateButton = $("#user-edit-updatebutton")
    const userEditDeleteButton = $("#user-edit-deletebutton")

    const userCreateEmail = $("#user-create-email")
    const userCreateFirstname = $("#user-create-firstname")
    const userCreateLastname = $("#user-create-lastname")
    const userCreateNetworkRating = $("#user-create-networkrating")
    const userCreatePilotRating = $("#user-create-pilotrating")
    const userCreatePassword = $("#user-create-password")
    const userCreateFSDPassword = $("#user-create-fsdpassword")

    const userCreateSubmitButton = $("#user-create-submitbutton")

    const userFetchButton = $("#user-fetch-button")
    const userCreateButton = $("#user-create-button")

    const userFetchCID = $("#user-fetch-cid")
    const userFetchInitiate = $("#user-fetch-initiate")

    const toastClipboardButton = $("#toast-clipboard-button")

    userFetchButton.click(() => {
        let modal = new bootstrap.Modal(userFetchModal)
        modal.show()
    })

    userCreateButton.click(() => {
        let modal = new bootstrap.Modal(userCreateModal)
        modal.show()
    })

    userFetchInitiate.click(() => {
        new bootstrap.Modal(userFetchModal).hide()

        if (userFetchCID.val() === "") {
            showResponse("Error: CID empty")
            return
        }

        new bootstrap.Modal(userFetchModal).hide()

        // Fetch users
        $.ajax({
            type: "GET",
            url: `/api/v1/users/${parseInt(userFetchCID.val())}`,
            dataType: "json",
            headers: {
                "Authorization": `Bearer ${token}`,
            },
            success: function (data, textStatus, jqXHR) {
                userEditCID.text(data.user.cid)
                userEditEmail.val(data.user.email)
                userEditFirstname.val(data.user.first_name)
                userEditLastname.val(data.user.last_name)
                userEditNetworkRating.val(data.user.network_rating)
                userEditNetworkRating.change()
                userEditPilotRating.val(data.user.pilot_rating)
                userEditPilotRating.change()
                userEditLastUpdated.text(data.user.updated_at)
                userEditCreatedAt.text(data.user.created_at)
                new bootstrap.Modal(userEditModal).show()
            },
            error: function (jqXHR, textStatus, errorThrown) {
                // If we received an error response from the server
                showResponse('Error: ' + jqXHR.statusText + '\nMessage: ' + JSON.parse(jqXHR.responseText).msg)
            }
        });
    })

    userEditUpdateButton.click(() => {

        if (!window.confirm(`Are you sure you want to update CID ${userEditCID.text()}?`)) {
            return
        }

        // Update user
        $.ajax({
            type: "PUT",
            url: `/api/v1/users`,
            contentType: "application/json",
            headers: {
                "Authorization": `Bearer ${token}`,
            },
            data: JSON.stringify({
                cid: parseInt(userEditCID.text()),
                user: {
                    cid: parseInt(userEditCID.text()),
                    email: userEditEmail.val(),
                    first_name: userEditFirstname.val(),
                    last_name: userEditLastname.val(),
                    network_rating: parseInt(userEditNetworkRating.val()),
                    pilot_rating: parseInt(userEditPilotRating.val()),
                    password: userEditPassword.val(),
                    fsd_password: userEditFSDPassword.val(),
                }
            }),
            success: function (data, textStatus, jqXHR) {
                showResponse(`Successfully updated user ${userEditCID.text()}`)
                new bootstrap.Modal(userEditModal).hide()
            },
            error: function (jqXHR, textStatus, errorThrown) {
                // If we received an error response from the server
                showResponse('Error: ' + jqXHR.statusText + '\nMessage: ' + JSON.parse(jqXHR.responseText).msg)
            }
        });
    })

    userEditDeleteButton.click(() => {

        if (!window.confirm(`Are you sure you want to delete CID ${userEditCID.text()}?`)) {
            return
        }

        // Delete user
        $.ajax({
            type: "DELETE",
            url: `/api/v1/users`,
            contentType: "application/json",
            headers: {
                "Authorization": `Bearer ${token}`,
            },
            data: JSON.stringify({
                cid: parseInt(userEditCID.text()),
            }),
            success: function (data, textStatus, jqXHR) {
                showResponse(`Successfully deleted user ${userEditCID.text()}`)
                new bootstrap.Modal(userEditModal).hide()
            },
            error: function (jqXHR, textStatus, errorThrown) {
                // If we received an error response from the server
                showResponse('Error: ' + jqXHR.statusText + '\nMessage: ' + JSON.parse(jqXHR.responseText).msg)
            }
        });
    })

    userCreateSubmitButton.click(() => {

        if (userCreatePassword === "" || userCreateFSDPassword === "") {
            showResponse("error: password is empty")
            return
        }
        
        // Create user
        $.ajax({
            type: "POST",
            url: `/api/v1/users`,
            contentType: "application/json",
            headers: {
                "Authorization": `Bearer ${token}`,
            },
            data: JSON.stringify({
                user: {
                    email: userCreateEmail.val(),
                    first_name: userCreateFirstname.val(),
                    last_name: userCreateLastname.val(),
                    network_rating: parseInt(userCreateNetworkRating.val()),
                    pilot_rating: parseInt(userCreatePilotRating.val()),
                    password: userCreatePassword.val(),
                    fsd_password: userCreateFSDPassword.val(),
                }
            }),
            success: function (data, textStatus, jqXHR) {

                let pwdMsg = userCreatePassword.val() === "" ? `\nPassword:     ${data.user.password}` : ``
                userCreateFSDPassword.val() === "" ? pwdMsg += `\nFSD Password: ${data.user.fsd_password}` : ``

                showResponse(`Successfully created user: CID = ${data.user.cid}\n${pwdMsg}`)
                new bootstrap.Modal(userCreateModal).hide()
            },
            error: function (jqXHR, textStatus, errorThrown) {
                // If we received an error response from the server
                showResponse('Error: ' + jqXHR.statusText + '\nMessage: ' + JSON.parse(jqXHR.responseText).msg)
            }
        });
    })

    toastClipboardButton.click(() => {
        let text = $("#responseBody").text()
        navigator.clipboard.writeText(text).then(r => {
            $("#toast-copy-banner").text("Copied!")
            setTimeout(() => {
                $("#toast-copy-banner").text("")
            }, 2000)
        });
    })
})