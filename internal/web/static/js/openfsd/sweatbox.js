// Sweatbox instructor control panel (vanilla JS, no jQuery).
//
// Progressive enhancement only:
// - Server-rendered table + forms remain the primary path (no-JS works).
// - Polls GET /api/v1/sweatbox/state every 1–2 s for status + aircraft table.
// - Persists draft fields in localStorage (callsign, command history, apt/air paste).
// - Commands, pause/unpause, load, delete stay as HTML form POSTs (CSRF).
//
// No SPA, no client router, no global store for server state.
// credentials: 'same-origin' for session cookie on fetch.

(function () {
    "use strict";

    var DEFAULT_POLL_MS = 1500;
    var MIN_POLL_MS = 1000;
    var MAX_POLL_MS = 5000;
    var STORAGE_KEY = "openfsd.sweatbox.v1";
    var HISTORY_MAX = 12;
    var SAVE_DEBOUNCE_MS = 200;

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

    // --- localStorage drafts (enhancement only; never source of truth for sim) ---

    function loadStore() {
        try {
            var raw = localStorage.getItem(STORAGE_KEY);
            if (!raw) {
                return {};
            }
            var obj = JSON.parse(raw);
            return obj && typeof obj === "object" ? obj : {};
        } catch (e) {
            return {};
        }
    }

    function saveStore(data) {
        try {
            localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
        } catch (e) {
            // Quota / private mode — ignore; forms still work.
        }
    }

    function readPersistFields(root) {
        var data = loadStore();
        var nodes = root.querySelectorAll("[data-persist]");
        for (var i = 0; i < nodes.length; i++) {
            var el = nodes[i];
            var key = el.getAttribute("data-persist");
            if (!key || !(key in data)) {
                continue;
            }
            var val = data[key];
            if (el.type === "checkbox") {
                el.checked = !!val;
            } else if (el.tagName === "TEXTAREA" || el.tagName === "INPUT") {
                // Do not overwrite non-empty values (e.g. browser restore).
                if (!el.value) {
                    el.value = val == null ? "" : String(val);
                }
            }
        }
        return data;
    }

    function writePersistFields(root, extra) {
        var data = loadStore();
        var nodes = root.querySelectorAll("[data-persist]");
        for (var i = 0; i < nodes.length; i++) {
            var el = nodes[i];
            var key = el.getAttribute("data-persist");
            if (!key) {
                continue;
            }
            // Command line: keep last typed draft, but history is separate.
            if (el.type === "checkbox") {
                data[key] = !!el.checked;
            } else {
                data[key] = el.value || "";
            }
        }
        if (extra && typeof extra === "object") {
            for (var k in extra) {
                if (Object.prototype.hasOwnProperty.call(extra, k)) {
                    data[k] = extra[k];
                }
            }
        }
        saveStore(data);
        return data;
    }

    function bindPersist(root) {
        var timer = null;
        function schedule() {
            if (timer) {
                clearTimeout(timer);
            }
            timer = setTimeout(function () {
                writePersistFields(root);
            }, SAVE_DEBOUNCE_MS);
        }
        root.addEventListener("input", function (ev) {
            var t = ev.target;
            if (t && t.getAttribute && t.getAttribute("data-persist")) {
                schedule();
            }
        });
        root.addEventListener("change", function (ev) {
            var t = ev.target;
            if (t && t.getAttribute && t.getAttribute("data-persist")) {
                schedule();
            }
        });
        readPersistFields(root);
    }

    // --- command history ---

    function getHistory() {
        var data = loadStore();
        return Array.isArray(data.history) ? data.history : [];
    }

    function pushHistory(cmd) {
        cmd = String(cmd || "").trim();
        if (!cmd) {
            return getHistory();
        }
        var hist = getHistory().filter(function (h) {
            return h !== cmd;
        });
        hist.unshift(cmd);
        if (hist.length > HISTORY_MAX) {
            hist = hist.slice(0, HISTORY_MAX);
        }
        var data = loadStore();
        data.history = hist;
        // Clear command draft after successful queue intent (form is navigating away).
        data.command = "";
        saveStore(data);
        return hist;
    }

    function renderHistory(root) {
        var list = root.querySelector("[data-js=cmd-history]");
        var input = root.querySelector("[data-js=command-input]") || document.getElementById("sbx-command");
        if (!list) {
            return;
        }
        var hist = getHistory();
        while (list.firstChild) {
            list.removeChild(list.firstChild);
        }
        if (!hist.length) {
            setHidden(list, true);
            return;
        }
        setHidden(list, false);
        for (var i = 0; i < hist.length; i++) {
            (function (cmd) {
                var li = document.createElement("li");
                var btn = document.createElement("button");
                btn.type = "button";
                btn.className = "btn btn-sm btn-outline-secondary";
                btn.textContent = cmd;
                btn.title = "Reuse: " + cmd;
                btn.addEventListener("click", function () {
                    if (input) {
                        input.value = cmd;
                        input.focus();
                        writePersistFields(root);
                    }
                });
                li.appendChild(btn);
                list.appendChild(li);
            })(hist[i]);
        }
    }

    function bindCommandForm(root) {
        var form = root.querySelector("[data-js=command-form]");
        if (!form) {
            return;
        }
        form.addEventListener("submit", function () {
            var input = form.querySelector("[name=command]");
            if (input && input.value) {
                pushHistory(input.value);
            }
            // Persist callsign / apt drafts before navigation.
            writePersistFields(root, { command: "" });
        });
    }

    // --- selection ---

    function selectedCallsign(root) {
        var input = document.getElementById("sbx-callsign");
        return input && input.value ? String(input.value).trim().toUpperCase() : "";
    }

    function setSelectedCallsign(root, cs, focusCmd) {
        var input = document.getElementById("sbx-callsign");
        if (input && cs) {
            input.value = cs;
            writePersistFields(root);
        }
        highlightSelectedRow(root);
        if (focusCmd) {
            var cmd = document.getElementById("sbx-command");
            if (cmd) {
                cmd.focus();
            }
        } else if (input) {
            input.focus();
        }
    }

    function highlightSelectedRow(root) {
        var cs = selectedCallsign(root);
        var rows = root.querySelectorAll("tbody[data-js=aircraft-tbody] tr[data-callsign]");
        for (var i = 0; i < rows.length; i++) {
            var row = rows[i];
            var rowCs = (row.getAttribute("data-callsign") || "").toUpperCase();
            if (cs && rowCs === cs) {
                row.classList.add("sbx-row-selected");
            } else {
                row.classList.remove("sbx-row-selected");
            }
        }
    }

    function bindCallsignInput(root) {
        var input = document.getElementById("sbx-callsign");
        if (!input) {
            return;
        }
        input.addEventListener("input", function () {
            highlightSelectedRow(root);
        });
    }

    /**
     * Build one aircraft table row with textContent only (never innerHTML for
     * user-controlled fields). Delete remains a form POST with CSRF.
     */
    function buildAircraftRow(root, ac, csrf) {
        var tr = document.createElement("tr");
        tr.setAttribute("data-callsign", ac.callsign || "");
        tr.tabIndex = 0;

        function tdText(val, className) {
            var td = document.createElement("td");
            if (className) {
                td.className = className;
            }
            td.textContent = val == null ? "" : String(val);
            return td;
        }

        tr.appendChild(tdText(ac.callsign, "sbx-cs"));
        tr.appendChild(tdText(ac.type));
        tr.appendChild(tdText(ac.rules));
        tr.appendChild(tdText(ac.squawk));
        tr.appendChild(tdText(formatInt(ac.heading)));
        tr.appendChild(tdText(formatInt(ac.alt)));
        tr.appendChild(tdText(formatInt(ac.speed)));
        tr.appendChild(tdText(ac.status));

        var tdInstr = tdText(ac.instruction, "sbx-instr");
        if (ac.instruction) {
            tdInstr.title = String(ac.instruction);
        }
        tr.appendChild(tdInstr);

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
        btn.textContent = "Del";
        form.appendChild(btn);

        tdAction.appendChild(form);
        tr.appendChild(tdAction);

        function selectFromRow(ev) {
            if (ev.target && ev.target.closest && ev.target.closest("form")) {
                return;
            }
            if (ac.callsign) {
                // Enter/click: select + focus command line (operator flow).
                setSelectedCallsign(root, ac.callsign, true);
            }
        }

        tr.addEventListener("click", selectFromRow);
        tr.addEventListener("keydown", function (ev) {
            if (ev.key === "Enter" || ev.key === " ") {
                ev.preventDefault();
                selectFromRow(ev);
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
                tbody.appendChild(buildAircraftRow(root, aircraft[i], csrf));
            }
        }

        var hasAircraft = aircraft.length > 0;
        setHidden(tableWrap, !hasAircraft);
        setHidden(emptyEl, hasAircraft);
        highlightSelectedRow(root);
    }

    function setLiveStatus(root, text, isError) {
        var el = root.querySelector("[data-js=live-status]");
        if (!el) {
            return;
        }
        el.textContent = text;
        el.classList.toggle("is-error", !!isError);
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
                setLiveStatus(root, "live: disabled", true);
                return;
            }
            if (res.status === 401 || res.status === 403) {
                setLiveStatus(root, "live: session expired", true);
                return;
            }
            if (!res.ok) {
                setLiveStatus(root, "live: err " + res.status, true);
                return;
            }
            var state = await res.json();
            applyState(root, state);
            setLiveStatus(root, "live", false);
        } catch (err) {
            setLiveStatus(root, "live: fail", true);
            if (typeof console !== "undefined" && console.error) {
                console.error("sweatbox poll failed:", err);
            }
        }
    }

    function bindSSRRows(root) {
        // Wire selection on server-rendered rows before first poll rebuild.
        var rows = root.querySelectorAll("tbody[data-js=aircraft-tbody] tr[data-callsign]");
        for (var i = 0; i < rows.length; i++) {
            (function (tr) {
                if (!tr.tabIndex && tr.tabIndex !== 0) {
                    tr.tabIndex = 0;
                }
                function selectFromRow(ev) {
                    if (ev.target && ev.target.closest && ev.target.closest("form")) {
                        return;
                    }
                    var cs = tr.getAttribute("data-callsign");
                    if (cs) {
                        setSelectedCallsign(root, cs, true);
                    }
                }
                tr.addEventListener("click", selectFromRow);
                tr.addEventListener("keydown", function (ev) {
                    if (ev.key === "Enter" || ev.key === " ") {
                        ev.preventDefault();
                        selectFromRow(ev);
                    }
                });
            })(rows[i]);
        }
        highlightSelectedRow(root);
    }

    function start(root) {
        var pollMs = parseInt(root.getAttribute("data-poll-ms") || "", 10);
        if (!isFinite(pollMs) || pollMs < MIN_POLL_MS) {
            pollMs = DEFAULT_POLL_MS;
        }
        if (pollMs > MAX_POLL_MS) {
            pollMs = MAX_POLL_MS;
        }

        bindPersist(root);
        bindCommandForm(root);
        bindCallsignInput(root);
        bindSSRRows(root);
        renderHistory(root);

        setLiveStatus(root, "live…", false);

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
