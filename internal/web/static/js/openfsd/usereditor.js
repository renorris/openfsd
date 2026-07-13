// Progressive enhancement for user editor: password strength meter only.
// Create / update / search use native form POST/GET — no JS required.

function evaluatePassword(password, strengthBar, feedback) {
    if (!password) {
        strengthBar.style.width = "0%";
        strengthBar.className = "progress-bar";
        strengthBar.setAttribute("aria-valuenow", "0");
        feedback.textContent = "";
        return;
    }

    let strength = 0;
    if (password.length >= 8) strength += 50;
    if (/[A-Z]/.test(password)) strength += 15;
    if (/[a-z]/.test(password)) strength += 15;
    if (/[0-9]/.test(password)) strength += 10;
    if (/[^A-Za-z0-9]/.test(password)) strength += 10;

    strength = Math.min(strength, 100);
    strengthBar.style.width = strength + "%";
    strengthBar.setAttribute("aria-valuenow", String(strength));

    if (strength < 60) {
        strengthBar.className = "progress-bar bg-danger";
        feedback.textContent = "Weak: Include uppercase, lowercase, numbers, or symbols.";
    } else if (strength < 80) {
        strengthBar.className = "progress-bar bg-warning";
        feedback.textContent = "Moderate: Add more character types for strength.";
    } else {
        strengthBar.className = "progress-bar bg-success";
        feedback.textContent = "Strong: Good password!";
    }
}

function bindPasswordMeter(inputId, barId, feedbackId) {
    const input = document.getElementById(inputId);
    const bar = document.getElementById(barId);
    const feedback = document.getElementById(feedbackId);
    if (!input || !bar || !feedback) {
        return;
    }
    const wrap = input.closest(".mb-3");
    if (wrap) {
        const pe = wrap.querySelector("[data-js=password-strength]");
        if (pe) {
            pe.hidden = false;
        }
    }
    input.addEventListener("input", function () {
        evaluatePassword(input.value, bar, feedback);
    });
}

document.addEventListener("DOMContentLoaded", function () {
    bindPasswordMeter("create-password", "create-password-strength", "create-password-feedback");
    bindPasswordMeter("edit-password", "edit-password-strength", "edit-password-feedback");
});
