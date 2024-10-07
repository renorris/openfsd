const showResponse = (msg) => {
    $('#responseBody').text(msg);
    const toast = new bootstrap.Toast($('#responseToast'));
    toast.show();
};

$(document).ready(function () {
    $('#change-primary-password-form').on('submit', function (event) {
        event.preventDefault(); // Prevent the default form submission

        // Get the values from the input fields
        const oldPassword = $('input[name="old-password-primary"]').val();
        const newPassword = $('input[name="new-password-primary"]').val();
        const newPasswordConfirm = $('input[name="new-password-confirm-primary"]').val();

        if (oldPassword === "" || newPassword === "" || newPasswordConfirm === "") {
            showResponse('Error: password empty');
            return;
        }

        // Check if new password and old password are equal
        if (newPassword !== newPasswordConfirm) {
            showResponse('Error: passwords do not match');
            return; // Exit the function if they are not equal
        }

        // Send AJAX POST request
        $.ajax({
            type: 'POST',
            url: '/changepassword',
            data: {
                old_password: oldPassword,
                new_password: newPassword,
                change_fsd_password: false
            },
            xhrFields: {
                withCredentials: true
            },
        }).done(function (data, textStatus, jqXHR) {
            $('input[name="old-password-primary"]').val("")
            $('input[name="new-password-primary"]').val("")
            $('input[name="new-password-confirm-primary"]').val("")
            showResponse('Password changed successfully')
        }).fail(function (jqXHR, textStatus, errorThrown) {
            // If we received an error response from the server
            showResponse('Error: ' + jqXHR.statusText + '\nResponse: ' + jqXHR.responseText); // Alert with the status code and response body
        })
    })

    $('#change-fsd-password-form').on('submit', function (event) {
        event.preventDefault(); // Prevent the default form submission

        // Get the values from the input fields
        const newPassword = $('input[name="new-password-fsd"]').val();
        const newPasswordConfirm = $('input[name="new-password-confirm-fsd"]').val();

        if (newPassword === "" || newPasswordConfirm === "") {
            showResponse('Error: password empty');
            return;
        }

        // Check if new password and old password are equal
        if (newPassword !== newPasswordConfirm) {
            showResponse('Error: passwords do not match');
            return; // Exit the function if they are not equal
        }

        // Send AJAX POST request
        $.ajax({
            type: 'POST',
            url: '/changepassword',
            data: {
                new_password: newPassword,
                change_fsd_password: true
            },
            xhrFields: {
                withCredentials: true // Include credentials (cookies, etc.) in the request
            },
        }).done(function (data, textStatus, jqXHR) {
            console.log('done')
            $('input[name="new-password-fsd"]').val("")
            $('input[name="new-password-confirm-fsd"]').val("")
            showResponse('FSD password changed successfully!')
        }).fail(function (jqXHR, textStatus, errorThrown) {
            // If we received an error response from the server
            showResponse('Error: ' + jqXHR.statusText + '\nResponse: ' + jqXHR.responseText); // Alert with the status code and response body
        })
    })
});