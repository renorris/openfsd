const keyLabels = {
    "WELCOME_MESSAGE": "Welcome Message",
};

// Function to show message modal
function showMessageModal(message, token) {
    const messageText = document.getElementById('messageText');
    if (token) {
        messageText.innerHTML = message + ' <div class="d-flex align-items-center"><code class="api-key me-2">' + token + '</code><button class="btn btn-sm btn-outline-secondary copy-btn">Copy</button></div>';
        const copyBtn = messageText.querySelector('.copy-btn');
        copyBtn.addEventListener('click', function() {
            navigator.clipboard.writeText(token).then(() => {
                copyBtn.textContent = 'Copied!';
                setTimeout(() => {
                    copyBtn.textContent = 'Copy';
                }, 2000);
            }).catch(err => {
                console.error('Failed to copy: ', err);
            });
        });
    } else {
        messageText.textContent = message;
    }
    const messageModal = new bootstrap.Modal(document.getElementById('messageModal'));
    messageModal.show();
}

async function loadConfig() {
    try {
        const res = await doAPIRequestWithAuth('GET', '/api/v1/config/load');
        if (res.err) {
            alert('Error: ' + res.err);
            return;
        }
        const configForm = document.getElementById('config-form');
        configForm.innerHTML = ''; // Clear existing fields
        res.data.key_value_pairs.forEach(kv => {
            const label = keyLabels[kv.key] || kv.key;
            const div = document.createElement('div');
            div.className = 'mb-3';
            div.innerHTML = `
                <label for="${kv.key}" class="form-label">${label}</label>
                <input type="text" class="form-control" id="${kv.key}" value="${kv.value}" data-key="${kv.key}">
            `;
            configForm.appendChild(div);
        });
    } catch (xhr) {
        const errMsg = xhr.responseJSON && xhr.responseJSON.err ? xhr.responseJSON.err : 'An error occurred';
        alert('Error: ' + errMsg);
        console.error('Request failed:', xhr);
    }
}

document.getElementById('add-config').addEventListener('click', function() {
    const configForm = document.getElementById('config-form');
    const div = document.createElement('div');
    div.className = 'mb-3 new-config';
    // Create dropdown options from keyLabels
    let options = '';
    Object.keys(keyLabels).forEach(key => {
        options += `<option value="${key}">${keyLabels[key]}</option>`;
    });
    div.innerHTML = `
        <label class="form-label">New Key</label>
        <select class="form-control mb-2" data-type="new-key">
            <option value="" disabled selected>Select a key</option>
            ${options}
        </select>
        <label class="form-label">Value</label>
        <input type="text" class="form-control" placeholder="Value" data-type="new-value">
    `;
    configForm.appendChild(div);
});

document.getElementById('save-config').addEventListener('click', async function() {
    const keyValuePairs = [];

    // Existing configs
    const existingInputs = document.querySelectorAll('#config-form input[data-key]');
    existingInputs.forEach(input => {
        keyValuePairs.push({
            key: input.getAttribute('data-key'),
            value: input.value
        });
    });

    // New configs
    const newConfigDivs = document.querySelectorAll('#config-form .new-config');
    newConfigDivs.forEach(div => {
        const keySelect = div.querySelector('select[data-type="new-key"]');
        const valueInput = div.querySelector('input[data-type="new-value"]');
        if (keySelect && valueInput && keySelect.value.trim() !== '') {
            keyValuePairs.push({
                key: keySelect.value,
                value: valueInput.value
            });
        }
    });

    try {
        const res = await doAPIRequestWithAuth('POST', '/api/v1/config/update', { key_value_pairs: keyValuePairs });
        if (res.err) {
            alert('Error: ' + res.err);
        } else {
            alert('Config updated successfully');
            loadConfig(); // Reload to show new configs if added
        }
    } catch (xhr) {
        const errMsg = xhr.responseJSON && xhr.responseJSON.err ? xhr.responseJSON.err : 'An error occurred';
        alert('Error: ' + errMsg);
        console.error('Request failed:', xhr);
    }
});

// Added function to reset the JWT Secret Key
async function resetJwtSecretKey() {
    try {
        const res = await doAPIRequestWithAuth('POST', '/api/v1/config/resetsecretkey');
        if (res.err) {
            alert('Error: ' + res.err);
        } else {
            alert('JWT Secret Key reset successfully');
        }
    } catch (xhr) {
        const errMsg = xhr.responseJSON && xhr.responseJSON.err ? xhr.responseJSON.err : 'An error occurred';
        alert('Error: ' + errMsg);
        console.error('Request failed:', xhr);
    }
}

// Added event listener for the reset button
document.addEventListener('DOMContentLoaded', function() {
    document.getElementById('reset-jwt-secret').addEventListener('click', function() {
        if (confirm('Are you sure you want to reset the JWT Secret Key? All previously generated API tokens and all active sessions will be invalidated.')) {
            resetJwtSecretKey();
        }
    });
});

// Modified event listener for create new API token button
document.addEventListener('DOMContentLoaded', function() {
    document.getElementById('create-new-api-key').addEventListener('click', function() {
        const createTokenModal = new bootstrap.Modal(document.getElementById('createTokenModal'));
        createTokenModal.show();
    });
});

// Added event listener for submit create token
document.addEventListener('DOMContentLoaded', function() {
    document.getElementById('submitCreateToken').addEventListener('click', async function() {
        const expiryDateStr = document.getElementById('expiryDate').value;
        let expiryDate;
        if (expiryDateStr) {
            const [year, month, day] = expiryDateStr.split('-').map(Number);
            if (isNaN(year) || isNaN(month) || isNaN(day)) {
                showMessageModal('Invalid date format.');
                return;
            }
            expiryDate = new Date(Date.UTC(year, month - 1, day));
            if (isNaN(expiryDate.getTime())) {
                showMessageModal('Invalid date.');
                return;
            }
        } else {
            expiryDate = new Date();
            expiryDate.setFullYear(expiryDate.getFullYear() + 1);
        }
        const expiryDateTime = expiryDate.toJSON();
        const data = {
            "expiry_date_time": expiryDateTime
        };
        try {
            const res = await doAPIRequestWithAuth('POST', '/api/v1/config/createtoken', data);
            if (res.err) {
                showMessageModal('Error: ' + res.err);
            } else {
                showMessageModal('API Token created:', res.data.token);
            }
        } catch (xhr) {
            const errMsg = xhr.responseJSON && xhr.responseJSON.err ? xhr.responseJSON.err : 'An error occurred';
            showMessageModal('Error: ' + errMsg);
        }
        // Hide the createTokenModal
        const createTokenModal = bootstrap.Modal.getInstance(document.getElementById('createTokenModal'));
        createTokenModal.hide();
    });
});

document.addEventListener('DOMContentLoaded', loadConfig);