# Violation Map

A runnable starter for a community-validated violation map. It uses:

- Vanilla JavaScript and Leaflet for the browser interface.
- Go for the API and static file server.
- PostgreSQL with PostGIS for spatial lookup and GeoJSON queries.

## Run locally

Requirements: Docker, Docker Compose, and Go 1.22 or newer.

```powershell
docker compose up -d
go mod download
go run .
```

Open [http://localhost:8080](http://localhost:8080).

The database initializer imports the 38 Indonesian province boundaries from
`data/provinces.json` and inserts three dummy baseline points. Province names
are read from each feature's `properties.WADMPR` field. To rerun the initializer
after editing the dataset:

```powershell
docker compose down -v
docker compose up -d
```

## API

| Method | Route | Purpose |
| --- | --- | --- |
| `GET` | `/api/provinces` | Return province polygons as GeoJSON. |
| `GET` | `/api/violations?calibrated=true` | Return baseline points plus verified submissions. |
| `GET` | `/api/violations?calibrated=false` | Return baseline dummy points only. |
| `POST` | `/api/violations` | Submit `lat`, `lng`, and an `image` as multipart form data. |
| `GET` | `/api/admin/violations` | Return the unverified review queue. |
| `PATCH` | `/api/admin/violations/{id}` | Set status to `verified` or `rejected`. |

## Configuration

The server accepts these environment variables:

| Variable | Default |
| --- | --- |
| `DATABASE_URL` | `postgres://violation_map:violation_map@localhost:5432/violation_map?sslmode=disable` |
| `UPLOAD_DIR` | `uploads` |
| `PORT` | `8080` |

For a production deployment, add authentication around `/api/admin/*`, place
uploads in durable object storage, and replace the included demonstration
province data.
