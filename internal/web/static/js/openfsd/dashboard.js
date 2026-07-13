// Dashboard map enhancement (vanilla JS). User identity is server-rendered;
// map polling is progressive enhancement only.

let userNetworkRating = 0;

async function kickUser(callsign) {
    try {
        await doAPIRequestWithAuth("POST", "/api/v1/fsdconn/kickuser", { callsign: callsign });
        alert("User kicked successfully");
    } catch (error) {
        alert("Failed to kick user");
    }
}

function networkRatingFromInt(val) {
    switch (val) {
        case -1: return "Inactive";
        case 0: return "Suspended";
        case 1: return "Observer";
        case 2: return "Student 1";
        case 3: return "Student 2";
        case 4: return "Student 3";
        case 5: return "Controller 1";
        case 6: return "Controller 2";
        case 7: return "Controller 3";
        case 8: return "Instructor 1";
        case 9: return "Instructor 2";
        case 10: return "Instructor 3";
        case 11: return "Supervisor";
        case 12: return "Administrator";
        default: return "Unknown";
    }
}

document.addEventListener("DOMContentLoaded", async () => {
    const root = document.getElementById("dashboard-root");
    if (root) {
        userNetworkRating = parseInt(root.dataset.networkRating || "0", 10) || 0;
    }

    const mapEl = document.getElementById("map");
    if (!mapEl || typeof L === "undefined") {
        return;
    }

    const map = L.map("map").setView([30, 0], 1);
    L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
        maxZoom: 19
    }).addTo(map);
    map.attributionControl.setPrefix("");

    const planeIcon = L.icon({
        iconUrl: "/static/images/plane.png",
        iconSize: [16, 16],
        iconAnchor: [8, 8],
    });

    await populateMap(map, planeIcon);
    setInterval(() => { populateMap(map, planeIcon); }, 15000);
});

let dashboardMarkers = [];

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
        dashboardMarkers.forEach((marker) => {
            if (marker.getPopup() && marker.getPopup().isOpen()) {
                openCallsigns.add(marker.options.title);
            }
        });

        dashboardMarkers.forEach((marker) => {
            map.removeLayer(marker);
        });
        dashboardMarkers = [];

        const pilots = (data && data.pilots) || [];
        pilots.forEach((pilot) => {
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

            // Build popup with text nodes / safe strings only for user fields.
            const wrap = document.createElement("div");
            const b = document.createElement("b");
            b.textContent = "Callsign: ";
            wrap.appendChild(b);
            wrap.appendChild(document.createTextNode(String(callsign)));
            wrap.appendChild(document.createElement("br"));
            wrap.appendChild(document.createTextNode(String(name || "")));
            wrap.appendChild(document.createElement("br"));
            wrap.appendChild(document.createTextNode(lat + " " + lon));
            if (userNetworkRating >= 11) {
                wrap.appendChild(document.createElement("br"));
                const btn = document.createElement("button");
                btn.type = "button";
                btn.textContent = "Kick";
                btn.addEventListener("click", () => kickUser(callsign));
                wrap.appendChild(btn);
            }
            marker.bindPopup(wrap);
            marker.addTo(map);
            dashboardMarkers.push(marker);

            if (openCallsigns.has(callsign)) {
                marker.openPopup();
            }
        });
        const countEl = document.getElementById("dashboard-connection-count");
        if (countEl) {
            countEl.textContent = String(dashboardMarkers.length);
        }
    } catch (error) {
        console.error("Failed to fetch pilot data:", error);
    }
}

// Keep label helper available for any future UI; networkRatingFromInt unused is OK.
void networkRatingFromInt;
