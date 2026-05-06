// 1. Initialize map and centers it on Indonesia
const map = L.map('map').setView([-0.789, 113.921], 5);

// 2. Add the OpenStreetMap base layer
L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 19,
    attribution: '&copy; OpenStreetMap contributors'
}).addTo(map);

// 3. Fetch and display the provincial data
fetch('./data/provinces.json')
    .then(response => response.json())
    .then(data => {
        L.geoJSON(data, {
            style: {
                color: "#ff0000", // Red outline
                weight: 1, 
                fillOpacity: 0.1
            }
        }).addTo(map);
    })
    .catch(error => console.error("Error loading provinces:", error));