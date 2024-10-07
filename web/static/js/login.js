$(document).ready(function () {
    $('#login-form').on('submit', function (event) {
        event.preventDefault(); // Prevent the default form submission

        // Get the values from the input fields
        const cid = $('input[name="cid"]').val();
        const password = $('input[name="password"]').val();

        // Send AJAX POST request
        $.ajax({
            type: 'POST',
            url: '/login',
            data: {
                cid: cid,
                password: password
            },
            xhrFields: {
                withCredentials: true // Include credentials (cookies, etc.) in the request
            },
            success: function (data, textStatus, jqXHR) {
                // If we received a successful response from the server (HTTP 200)
                if (jqXHR.status === 204) {
                    window.location.href = '/dashboard'; // Redirect to dashboard
                }
            },
            error: function (jqXHR, textStatus, errorThrown) {
                // If we received an error response from the server
                alert('Error: ' + jqXHR.statusText + '\nMessage: ' + jqXHR.responseText); // Alert with the status code and response body
            }
        });
    });
});
