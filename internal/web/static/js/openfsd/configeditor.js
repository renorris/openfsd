// Progressive enhancement for config editor.
// Save / create-token / reset-secret use native form POST — no JS required.
// This module only un-hides a "Copy token" button after a successful create.

document.addEventListener("DOMContentLoaded", function () {
    const copyBtn = document.querySelector("[data-js=copy-token]");
    const tokenEl = document.getElementById("created-token-value");
    if (!copyBtn || !tokenEl) {
        return;
    }
    copyBtn.hidden = false;
    copyBtn.addEventListener("click", function () {
        const text = tokenEl.textContent || "";
        if (!text || !navigator.clipboard || !navigator.clipboard.writeText) {
            return;
        }
        navigator.clipboard.writeText(text).then(function () {
            copyBtn.textContent = "Copied!";
            setTimeout(function () {
                copyBtn.textContent = "Copy token";
            }, 2000);
        }).catch(function () {
            // Clipboard may be denied; token remains visible for manual copy.
        });
    });
});
