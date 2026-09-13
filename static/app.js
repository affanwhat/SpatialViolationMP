const state = {
  calibrated: false,
  violationsLayer: null,
  selectionMarker: null,
};

const map = L.map("map").setView([-6.9, 108.3], 7);
L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
  attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
  maxZoom: 19,
}).addTo(map);

const provincesLayer = L.geoJSON(null, {
  style: {
    color: "#22665b",
    fillColor: "#5a9d8c",
    fillOpacity: 0.14,
    weight: 2,
  },
  onEachFeature(feature, layer) {
    layer.bindTooltip(feature.properties.name);
  },
}).addTo(map);

async function fetchJSON(url, options) {
  const response = await fetch(url, options);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || `Request failed with status ${response.status}`);
  }
  return data;
}

async function loadProvinces() {
  const data = await fetchJSON("/api/provinces");
  provincesLayer.addData(data);
  if (provincesLayer.getBounds().isValid()) {
    map.fitBounds(provincesLayer.getBounds(), { padding: [12, 12] });
  }
}

async function loadViolations() {
  const status = document.querySelector("#map-status");
  status.textContent = "Loading violation points...";
  try {
    const data = await fetchJSON(`/api/violations?calibrated=${state.calibrated}`);
    if (state.violationsLayer) {
      state.violationsLayer.remove();
    }
    state.violationsLayer = L.geoJSON(data, {
      pointToLayer(feature, latlng) {
        const color = feature.properties.is_dummy ? "#d96a38" : "#22665b";
        return L.circleMarker(latlng, {
          radius: 7,
          color: "#fff",
          fillColor: color,
          fillOpacity: 0.92,
          weight: 2,
        });
      },
      onEachFeature(feature, layer) {
        const props = feature.properties;
        const label = props.is_dummy ? "Original baseline point" : "Verified user report";
        layer.bindPopup(`<strong>${label}</strong><br>${props.province || "Outside mapped provinces"}`);
      },
    }).addTo(map);
    status.textContent = `${data.features.length} visible violation point${data.features.length === 1 ? "" : "s"}.`;
  } catch (error) {
    status.textContent = error.message;
  }
}

map.on("click", (event) => {
  const { lat, lng } = event.latlng;
  document.querySelector("#lat").value = lat.toFixed(6);
  document.querySelector("#lng").value = lng.toFixed(6);
  if (state.selectionMarker) {
    state.selectionMarker.setLatLng(event.latlng);
  } else {
    state.selectionMarker = L.marker(event.latlng).addTo(map);
  }
});

document.querySelectorAll(".view-toggle").forEach((button) => {
  button.addEventListener("click", () => {
    document.querySelector(".view-toggle.active").classList.remove("active");
    button.classList.add("active");
    state.calibrated = button.dataset.calibrated === "true";
    loadViolations();
  });
});

document.querySelector("#submission-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const status = document.querySelector("#submission-status");
  status.textContent = "Uploading report...";
  try {
    await fetchJSON("/api/violations", {
      method: "POST",
      body: new FormData(form),
    });
    form.reset();
    if (state.selectionMarker) {
      state.selectionMarker.remove();
      state.selectionMarker = null;
    }
    status.textContent = "Report submitted. It will appear after admin verification.";
  } catch (error) {
    status.textContent = error.message;
  }
});

document.querySelectorAll(".tab").forEach((button) => {
  button.addEventListener("click", () => {
    document.querySelector(".tab.active").classList.remove("active");
    document.querySelector(".panel.active").classList.remove("active");
    button.classList.add("active");
    document.querySelector(`#${button.dataset.panel}`).classList.add("active");
    if (button.dataset.panel === "admin-panel") {
      loadAdminQueue();
    } else {
      setTimeout(() => map.invalidateSize(), 0);
    }
  });
});

function createActionButton(label, id, status) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = `action-button${status === "rejected" ? " reject" : ""}`;
  button.textContent = label;
  button.addEventListener("click", () => reviewViolation(id, status));
  return button;
}

async function reviewViolation(id, status) {
  const adminStatus = document.querySelector("#admin-status");
  adminStatus.textContent = `Updating report #${id}...`;
  try {
    await fetchJSON(`/api/admin/violations/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ status }),
    });
    adminStatus.textContent = `Report #${id} marked ${status}.`;
    await loadAdminQueue();
    if (state.calibrated) {
      await loadViolations();
    }
  } catch (error) {
    adminStatus.textContent = error.message;
  }
}

async function loadAdminQueue() {
  const rows = document.querySelector("#admin-rows");
  const status = document.querySelector("#admin-status");
  status.textContent = "Loading pending reports...";
  try {
    const data = await fetchJSON("/api/admin/violations");
    rows.replaceChildren();
    if (data.features.length === 0) {
      const row = document.createElement("tr");
      const cell = document.createElement("td");
      cell.colSpan = 6;
      cell.className = "empty-row";
      cell.textContent = "No reports are waiting for review.";
      row.append(cell);
      rows.append(row);
    }
    data.features.forEach(({ geometry, properties }) => {
      const row = document.createElement("tr");
      const values = [
        properties.id,
        properties.province || "Outside mapped provinces",
        `${geometry.coordinates[1].toFixed(5)}, ${geometry.coordinates[0].toFixed(5)}`,
      ];
      values.forEach((value) => {
        const cell = document.createElement("td");
        cell.textContent = value;
        row.append(cell);
      });

      const imageCell = document.createElement("td");
      const link = document.createElement("a");
      link.href = properties.image_path;
      link.target = "_blank";
      link.rel = "noreferrer";
      link.textContent = "View image";
      imageCell.append(link);
      row.append(imageCell);

      const submittedCell = document.createElement("td");
      submittedCell.textContent = new Date(properties.created_at).toLocaleString();
      row.append(submittedCell);

      const actionsCell = document.createElement("td");
      actionsCell.append(
        createActionButton("Approve", properties.id, "verified"),
        createActionButton("Reject", properties.id, "rejected"),
      );
      row.append(actionsCell);
      rows.append(row);
    });
    status.textContent = `${data.features.length} pending report${data.features.length === 1 ? "" : "s"}.`;
  } catch (error) {
    status.textContent = error.message;
  }
}

document.querySelector("#refresh-admin").addEventListener("click", loadAdminQueue);

Promise.all([loadProvinces(), loadViolations()]).catch((error) => {
  document.querySelector("#map-status").textContent = error.message;
});
