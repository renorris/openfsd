// Dashboard map enhancement (vanilla JS, no jQuery).
//
// JS budget exception: this route may load Leaflet (vendor) + this module.
// User identity and connection summary are server-rendered; map polling is
// progressive enhancement only. credentials: 'same-origin' so the session
// cookie authenticates datafeed fetches (no localStorage Bearer).

let userNetworkRating = 0;
let dashboardMarkers = [];

async function kickUser(callsign) {
    try {
        await doAPIRequestWithAuth("POST", "/api/v1/fsdconn/kickuser", { callsign: callsign });
        alert("User kicked successfully");
    } catch (error) {
        alert("Failed to kick user");
    }
}

document.addEventListener("DOMContentLoaded", async function () {
    const root = document.getElementById("dashboard-root");
    if (root) {
        userNetworkRating = parseInt(root.dataset.networkRating || "0", 10) || 0;
    }

    const mapEl = document.getElementById("map");
    if (!mapEl || typeof L === "undefined") {
        // Map enhancement unavailable; server-rendered summary already present.
        return;
    }

    const map = L.map("map").setView([30, 0], 1);
    map.attributionControl.setPrefix("");

    // Theme-aware OSM basemap (light Standard / dark CARTO). Shared with
    // airport editor via OpenFSDTheme (theme.js).
    let baseLayer = null;
    function applyDashboardBasemap(theme) {
        const next =
            typeof OpenFSDTheme !== "undefined" &&
            typeof OpenFSDTheme.createLeafletOsmLayer === "function"
                ? OpenFSDTheme.createLeafletOsmLayer(L, theme)
                : L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
                    maxZoom: 19,
                    attribution:
                        '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
                });
        if (baseLayer) {
            map.removeLayer(baseLayer);
        }
        baseLayer = next;
        baseLayer.addTo(map);
    }

    const initialTheme =
        typeof OpenFSDTheme !== "undefined" &&
        typeof OpenFSDTheme.currentTheme === "function"
            ? OpenFSDTheme.currentTheme(document)
            : (document.documentElement.getAttribute("data-bs-theme") === "dark"
                ? "dark"
                : "light");
    applyDashboardBasemap(initialTheme);

    const themeEvent =
        typeof OpenFSDTheme !== "undefined" && OpenFSDTheme.THEME_CHANGE_EVENT
            ? OpenFSDTheme.THEME_CHANGE_EVENT
            : "openfsd:themechange";
    document.addEventListener(themeEvent, function (ev) {
        const theme =
            (ev && ev.detail && ev.detail.theme) ||
            (typeof OpenFSDTheme !== "undefined" &&
            typeof OpenFSDTheme.currentTheme === "function"
                ? OpenFSDTheme.currentTheme(document)
                : "light");
        applyDashboardBasemap(theme);
    });

    const planeIcon = L.icon({
        iconUrl: "/static/images/plane.png",
        iconSize: [16, 16],
        iconAnchor: [8, 8],
    });

    await populateMap(map, planeIcon);
    setInterval(function () {
        populateMap(map, planeIcon);
    }, 15000);
});

async function populateMap(map, planeIcon) {
    try {
        const res = await fetch("/api/v1/data/openfsd-data.json", {
            method: "GET",
            credentials: "same-origin",
            headers: { "Accept": "application/json" },
        });
        if (!res.ok) {
            throw new Error("datafeed " + res.status);
        }
        const data = await res.json();

        const openCallsigns = new Set();
        dashboardMarkers.forEach(function (marker) {
            if (marker.getPopup() && marker.getPopup().isOpen()) {
                openCallsigns.add(marker.options.title);
            }
        });

        dashboardMarkers.forEach(function (marker) {
            map.removeLayer(marker);
        });
        dashboardMarkers = [];

        const pilots = (data && data.pilots) || [];
        pilots.forEach(function (pilot) {
            const callsign = pilot.callsign;
            const lat = pilot.latitude;
            const lon = pilot.longitude;
            const heading = pilot.heading;
            const name = pilot.name;

            const marker = L.marker([lat, lon], {
                icon: planeIcon,
                rotationAngle: heading,
                rotationOrigin: "center center",
                title: callsign
            });

            // Build popup with text nodes only (never innerHTML with user fields).
            const wrap = document.createElement("div");
            const b = document.createElement("b");
            b.textContent = "Callsign: ";
            wrap.appendChild(b);
            wrap.appendChild(document.createTextNode(String(callsign)));
            if (pilot.synthetic) {
                wrap.appendChild(document.createTextNode(" "));
                const badge = document.createElement("span");
                badge.textContent = "[sweatbox]";
                badge.setAttribute("title", "In-process sweatbox pilot");
                wrap.appendChild(badge);
            }
            wrap.appendChild(document.createElement("br"));
            wrap.appendChild(document.createTextNode(String(name || "")));
            wrap.appendChild(document.createElement("br"));
            wrap.appendChild(document.createTextNode(lat + " " + lon));
            if (userNetworkRating >= 11) {
                wrap.appendChild(document.createElement("br"));
                const btn = document.createElement("button");
                btn.type = "button";
                btn.textContent = "Kick";
                btn.addEventListener("click", function () {
                    kickUser(callsign);
                });
                wrap.appendChild(btn);
            }
            marker.bindPopup(wrap);
            marker.addTo(map);
            dashboardMarkers.push(marker);

            if (openCallsigns.has(callsign)) {
                marker.openPopup();
            }
        });

        // Optionally refresh the count as enhancement (table stays server-rendered
        // until full page reload). Use textContent only.
        const countEl = document.querySelector("[data-js=connection-count]");
        if (countEl && countEl.tagName === "SPAN") {
            const atc = (data && data.controllers) || (data && data.atc) || [];
            const total = pilots.length + (Array.isArray(atc) ? atc.length : 0);
            countEl.textContent = String(total);
        }
    } catch (error) {
        console.error("Failed to fetch pilot data:", error);
    }
}
