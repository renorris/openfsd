// Sweatbox instructor live refresh (vanilla JS, no jQuery).
//
// Progressive enhancement only: server-rendered table + forms remain the
// primary path. This module polls GET /api/v1/sweatbox/state every 1–2 s and
// updates status fields + the aircraft table. Commands, pause/unpause, load,
// and delete stay as HTML form POSTs (server authority + CSRF). No SPA, no
// client router, no global store.
//
// credentials: 'same-origin' so the session cookie authenticates fetches.
// No Leaflet on this page (first-party poll only; map left optional).

(function () {
    "use strict";

    var DEFAULT_POLL_MS = 1500;
    var MIN_POLL_MS = 1000;
    var MAX_POLL_MS = 5000;

    function formatElapsed(sec) {
        sec = Math.max(0, Math.floor(Number(sec) || 0));
        var h = Math.floor(sec / 3600);
        var m = Math.floor((sec % 3600) / 60);
        var r = sec % 60;
        return h + ":" + pad2(m) + ":" + pad2(r);
    }

    function pad2(n) {
        return n < 10 ? "0" + n : String(n);
    }

    function formatInt(v) {
        var n = Number(v);
        if (!isFinite(n)) {
            return "0";
        }
        return String(Math.round(n));
    }

    function getCSRFToken() {
        var meta = document.querySelector('meta[name="csrf-token"]');
        if (meta && meta.content) {
            return meta.content;
        }
        var match = document.cookie.match(/(?:^|;\s*)openfsd_csrf=([^;]+)/);
        return match ? decodeURIComponent(match[1]) : "";
    }

    function setText(el, text) {
        if (el) {
            el.textContent = text == null ? "" : String(text);
        }
    }

    function setHidden(el, hidden) {
        if (!el) {
            return;
        }
        if (hidden) {
            el.setAttribute("hidden", "");
        } else {
            el.removeAttribute("hidden");
        }
    }

    function setButtonDisabled(btn, disabled) {
        if (!btn) {
            return;
        }
        if (disabled) {
            btn.setAttribute("disabled", "");
        } else {
            btn.removeAttribute("disabled");
        }
    }

    /**
     * Build one aircraft table row with textContent only (never innerHTML for
     * user-controlled fields). Delete remains a form POST with CSRF.
     */
    function buildAircraftRow(ac, csrf) {
        var tr = document.createElement("tr");
        tr.setAttribute("data-callsign", ac.callsign || "");

        function tdText(val) {
            var td = document.createElement("td");
            td.textContent = val == null ? "" : String(val);
            return td;
        }

        tr.appendChild(tdText(ac.callsign));
        tr.appendChild(tdText(ac.type));
        tr.appendChild(tdText(ac.rules));
        tr.appendChild(tdText(ac.squawk));
        tr.appendChild(tdText(formatInt(ac.heading)));
        tr.appendChild(tdText(formatInt(ac.alt)));
        tr.appendChild(tdText(formatInt(ac.speed)));
        tr.appendChild(tdText(ac.status));
        tr.appendChild(tdText(ac.instruction));

        var tdAction = document.createElement("td");
        var form = document.createElement("form");
        form.method = "post";
        form.action = "/sweatbox/delete";
        form.className = "d-inline m-0";

        var csrfInput = document.createElement("input");
        csrfInput.type = "hidden";
        csrfInput.name = "csrf_token";
        csrfInput.value = csrf || "";
        form.appendChild(csrfInput);

        var csInput = document.createElement("input");
        csInput.type = "hidden";
        csInput.name = "callsign";
        csInput.value = ac.callsign || "";
        form.appendChild(csInput);

        var btn = document.createElement("button");
        btn.type = "submit";
        btn.className = "btn btn-sm btn-outline-danger";
        btn.textContent = "Delete";
        form.appendChild(btn);

        tdAction.appendChild(form);
        tr.appendChild(tdAction);

        // PE: click row (except the delete form) to fill selected callsign.
        tr.addEventListener("click", function (ev) {
            if (ev.target && ev.target.closest && ev.target.closest("form")) {
                return;
            }
            var input = document.getElementById("sbx-callsign");
            if (input && ac.callsign) {
                input.value = ac.callsign;
                input.focus();
            }
        });

        return tr;
    }

    function applyState(root, state) {
        var icao = state.icao || "";
        setText(root.querySelector("[data-js=icao]"), icao || "—");
        setText(root.querySelector("[data-js=paused]"), state.paused ? "yes" : "no");
        setText(root.querySelector("[data-js=elapsed]"), formatElapsed(state.elapsed_sec));
        setText(
            root.querySelector("[data-js=arr-dep]"),
            String(state.arr_count || 0) + " / " + String(state.dep_count || 0)
        );

        setButtonDisabled(root.querySelector("[data-js=pause-btn]"), !!state.paused);
        setButtonDisabled(root.querySelector("[data-js=unpause-btn]"), !state.paused);

        var statusMsg = root.querySelector("[data-js=status-msg]");
        if (statusMsg) {
            if (!icao) {
                statusMsg.textContent = "No airport loaded. Upload or paste a .apt file to begin.";
                setHidden(statusMsg, false);
            } else {
                // Clear the "no airport" hint once an airport is present; leave
                // other server-rendered notices alone only when empty.
                if (statusMsg.textContent.indexOf("No airport loaded") === 0) {
                    statusMsg.textContent = "";
                    setHidden(statusMsg, true);
                }
            }
        }

        var aircraft = Array.isArray(state.aircraft) ? state.aircraft : [];
        var tbody = root.querySelector("[data-js=aircraft-tbody]");
        var tableWrap = root.querySelector("[data-js=aircraft-table-wrap]");
        var emptyEl = root.querySelector("[data-js=aircraft-empty]");
        var csrf = getCSRFToken();

        if (tbody) {
            while (tbody.firstChild) {
                tbody.removeChild(tbody.firstChild);
            }
            for (var i = 0; i < aircraft.length; i++) {
                tbody.appendChild(buildAircraftRow(aircraft[i], csrf));
            }
        }

        var hasAircraft = aircraft.length > 0;
        setHidden(tableWrap, !hasAircraft);
        setHidden(emptyEl, hasAircraft);
    }

    function setLiveStatus(root, text, isError) {
        var el = root.querySelector("[data-js=live-status]");
        if (!el) {
            return;
        }
        el.textContent = text;
        el.classList.toggle("text-danger", !!isError);
        el.classList.toggle("text-muted", !isError);
        setHidden(el, false);
    }

    async function pollOnce(root) {
        try {
            var res = await fetch("/api/v1/sweatbox/state", {
                method: "GET",
                credentials: "same-origin",
                headers: { Accept: "application/json" },
            });
            if (res.status === 404) {
                setLiveStatus(root, "Live: sweatbox disabled on FSD", true);
                return;
            }
            if (res.status === 401 || res.status === 403) {
                setLiveStatus(root, "Live: session expired — reload page", true);
                return;
            }
            if (!res.ok) {
                setLiveStatus(root, "Live: control plane error (" + res.status + ")", true);
                return;
            }
            var state = await res.json();
            applyState(root, state);
            setLiveStatus(root, "Live · updating", false);
        } catch (err) {
            setLiveStatus(root, "Live: refresh failed", true);
            if (typeof console !== "undefined" && console.error) {
                console.error("sweatbox poll failed:", err);
            }
        }
    }

    function start(root) {
        var pollMs = parseInt(root.getAttribute("data-poll-ms") || "", 10);
        if (!isFinite(pollMs) || pollMs < MIN_POLL_MS) {
            pollMs = DEFAULT_POLL_MS;
        }
        if (pollMs > MAX_POLL_MS) {
            pollMs = MAX_POLL_MS;
        }

        setLiveStatus(root, "Live · connecting…", false);

        // Initial refresh soon after load (SSR already shows a snapshot).
        pollOnce(root);
        setInterval(function () {
            pollOnce(root);
        }, pollMs);
    }

    document.addEventListener("DOMContentLoaded", function () {
        var root = document.querySelector("[data-js=sweatbox]");
        if (!root) {
            return;
        }
        // Only enhance when the control plane was available at render time.
        if (root.getAttribute("data-available") !== "1") {
            return;
        }
        start(root);
    });
})();
