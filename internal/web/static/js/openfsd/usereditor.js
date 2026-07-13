// Form Handlers
document.getElementById('search-form').addEventListener('submit', async function(event) {
    event.preventDefault();
    const cid = document.getElementById('search-cid').value;
    try {
        const res = await doAPIRequestWithAuth('POST', '/api/v1/user/load', {cid: parseInt(cid)});
        if (res.err) {
            alert('Error: ' + res.err);
        } else {
            const user = res.data;
            document.getElementById('edit-cid').value = user.cid;
            document.getElementById('edit-cid').hidden = false;
            document.getElementById('edit-first-name').value = user.first_name || '';
            document.getElementById('edit-last-name').value = user.last_name || '';
            document.getElementById('edit-network-rating').value = user.network_rating;
            document.getElementById('edit-password').value = '';
        }
    } catch (xhr) {
        const errMsg = xhr.responseJSON && xhr.responseJSON.err ? xhr.responseJSON.err : 'An error occurred';
        alert('Error: ' + errMsg);
        console.error('Request failed:', xhr);
    }
});

document.getElementById('create-form').addEventListener('submit', async function(event) {
    event.preventDefault();
    const firstName = document.getElementById('create-first-name').value;
    const lastName = document.getElementById('create-last-name').value;
    const password = document.getElementById('create-password').value;
    const networkRating = document.getElementById('create-network-rating').value;
    const data = {
        password: password,
        network_rating: parseInt(networkRating)
    };
    if (firstName) data.first_name = firstName;
    if (lastName) data.last_name = lastName;
    try {
        const res = await doAPIRequestWithAuth('POST', '/api/v1/user/create', data);
        if (res.err) {
            alert('Error: ' + res.err);
        } else {
            alert('User created successfully. CID: ' + res.data.cid);
            document.getElementById('create-first-name').value = '';
            document.getElementById('create-last-name').value = '';
            document.getElementById('create-password').value = '';
            document.getElementById('create-network-rating').value = '-1';
        }
    } catch (xhr) {
        const errMsg = xhr.responseJSON && xhr.responseJSON.err ? xhr.responseJSON.err : 'An error occurred';
        alert('Error: ' + errMsg);
        console.error('Request failed:', xhr);
    }
});

document.getElementById('edit-form').addEventListener('submit', async function(event) {
    event.preventDefault();
    const cid = document.getElementById('edit-cid').value;
    const firstName = document.getElementById('edit-first-name').value;
    const lastName = document.getElementById('edit-last-name').value;
    const networkRating = document.getElementById('edit-network-rating').value;
    const password = document.getElementById('edit-password').value;
    const data = {
        cid: parseInt(cid),
        first_name: firstName,
        last_name: lastName,
        network_rating: parseInt(networkRating)
    };
    if (password) data.password = password;
    try {
        const res = await doAPIRequestWithAuth('PATCH', '/api/v1/user/update', data);
        if (res.err) {
            alert('Error: ' + res.err);
        } else {
            alert('User updated successfully');
        }
    } catch (xhr) {
        const errMsg = xhr.responseJSON && xhr.responseJSON.err ? xhr.responseJSON.err : 'An error occurred';
        alert('Error: ' + errMsg);
        console.error('Request failed:', xhr);
    }
});

document.addEventListener('DOMContentLoaded', () => {
    const createPasswordInput = document.getElementById('create-password');
    const createStrengthBar = document.getElementById('create-password-strength');
    const createFeedback = document.getElementById('create-password-feedback');
    const editPasswordInput = document.getElementById('edit-password');
    const editStrengthBar = document.getElementById('edit-password-strength');
    const editFeedback = document.getElementById('edit-password-feedback');

    const evaluatePassword = (password, strengthBar, feedback) => {
        if (!password) {
            strengthBar.style.width = '0%';
            strengthBar.className = 'progress-bar';
            feedback.textContent = '';
            return;
        }

        let strength = 0;
        if (password.length === 8) strength += 50;
        if (/[A-Z]/.test(password)) strength += 15;
        if (/[a-z]/.test(password)) strength += 15;
        if (/[0-9]/.test(password)) strength += 10;
        if (/[^A-Za-z0-9]/.test(password)) strength += 10;

        strength = Math.min(strength, 100);
        strengthBar.style.width = `${strength}%`;

        if (strength < 60) {
            strengthBar.className = 'progress-bar bg-danger';
            feedback.textContent = 'Weak: Include uppercase, lowercase, numbers, or symbols.';
        } else if (strength < 80) {
            strengthBar.className = 'progress-bar bg-warning';
            feedback.textContent = 'Moderate: Add more character types for strength.';
        } else {
            strengthBar.className = 'progress-bar bg-success';
            feedback.textContent = 'Strong: Good password!';
        }
    };

    createPasswordInput.addEventListener('input', () => {
        evaluatePassword(createPasswordInput.value, createStrengthBar, createFeedback);
    });

    editPasswordInput.addEventListener('input', () => {
        evaluatePassword(editPasswordInput.value, editStrengthBar, editFeedback);
    });
});