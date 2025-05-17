function networkRatingFromInt(val) {
    switch (val) {
        case -1: return "Inactive"
        case 0: return "Suspended"
        case 1: return "Observer"
        case 2: return "Student 1"
        case 3: return "Student 2"
        case 4: return "Student 3"
        case 5: return "Controller 1"
        case 6: return "Controller 2"
        case 7: return "Controller 3"
        case 8: return "Instructor 1"
        case 9: return "Instructor 2"
        case 10: return "Instructor 3"
        case 11: return "Supervisor"
        case 12: return "Administrator"
        default: return "Unknown"
    }
}

$(document).ready(async () => {
    const claims = getAccessTokenClaims()
    loadUserInfo(claims.cid)

    const map = L.map('map').setView([30, 0], 1);
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxZoom: 19
    }).addTo(map);
    map.attributionControl.setPrefix('')

    const planeIcon = L.icon({
        iconUrl: '/static/images/plane.png',
        iconSize: [16, 16], // Adjust based on your icon's dimensions
        iconAnchor: [8, 8], // Center of the icon
    });

    await populateMap(map, planeIcon)
    setInterval(() => { populateMap(map, planeIcon) }, 5000)
})

async function loadUserInfo(cid) {
    const res = await doAPIRequest("POST", "/api/v1/user/load", true, { cid: cid })

    if (res.data.first_name !== "") {
        $("#dashboard-real-name").text(`Welcome, ${res.data.first_name}!`);
    } else {
        $("#dashboard-real-name").text("Welcome!")
    }

    $("#dashboard-network-rating").text(networkRatingFromInt(res.data.network_rating))
    $("#dashboard-cid").text(`CID: ${res.data.cid}`)

    if (res.data.network_rating >= 11) {
        $("#dashboard-user-editor").html(`
            <div class="mb-2"><a href="/usereditor" class="btn btn-primary">Edit Users</a></div>
            <div class="mb-2"><a href="/configeditor" class="btn btn-primary">Configure Server</a></div>
        `)
    }
}

let dashboardMarkers = [];

async function populateMap(map, planeIcon) {
    try {
        const res = await $.ajax("https://data.vatsim.net/v3/vatsim-data.json", {
            method: "GET",
            dataType: "json"
        });

        // Remove existing markers
        dashboardMarkers.forEach((marker) => {
            map.removeLayer(marker);
        });
        dashboardMarkers = [];

        res.pilots.forEach((pilot) => {
            const callsign = pilot.callsign;
            const lat = pilot.latitude;
            const lon = pilot.longitude;
            const heading = pilot.heading;
            const name = pilot.name;

            const marker = L.marker([lat, lon], { icon: planeIcon, title: callsign });
            // Bind popup with callsign
            marker.bindPopup(`<b>Callsign:</b> ${callsign}<br>${name}<br>${lat} ${lon}`).openPopup();
            marker.on('click', function() {
                this.openPopup();
            });
            marker.addTo(map);
            // Set rotation
            if (marker._icon) {
                const currentTransform = marker._icon.style.transform || '';
                marker._icon.style.transform = currentTransform + ` rotate(${heading}deg)`;
                marker._icon.style.transformOrigin = "center"
            }
            dashboardMarkers.push(marker);
        });
        $("#dashboard-connection-count").text(dashboardMarkers.length);
    } catch (error) {
        console.error("Failed to fetch VATSIM data:", error);
    }
}
