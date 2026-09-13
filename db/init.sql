CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE provinces (
    id SERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL UNIQUE,
    geom geometry(MultiPolygon, 4326) NOT NULL
);

CREATE INDEX provinces_geom_idx ON provinces USING GIST (geom);

CREATE TABLE violations (
    id BIGSERIAL PRIMARY KEY,
    province_id INTEGER REFERENCES provinces(id),
    geom geometry(Point, 4326) NOT NULL,
    image_path TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'unverified'
        CHECK (status IN ('unverified', 'verified', 'rejected')),
    is_dummy BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX violations_geom_idx ON violations USING GIST (geom);
CREATE INDEX violations_status_idx ON violations (status);

INSERT INTO provinces (name, geom)
SELECT
    feature -> 'properties' ->> 'WADMPR',
    ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(feature -> 'geometry'), 4326))
FROM jsonb_array_elements(
    (pg_read_file('/data/provinces.json')::jsonb) -> 'features'
) AS feature;

WITH dummy_points (geom) AS (
    VALUES
        (ST_SetSRID(ST_MakePoint(106.82, -6.18), 4326)),
        (ST_SetSRID(ST_MakePoint(107.61, -6.91), 4326)),
        (ST_SetSRID(ST_MakePoint(110.37, -7.80), 4326))
)
INSERT INTO violations (province_id, geom, status, is_dummy)
SELECT p.id, d.geom, 'verified', TRUE
FROM dummy_points d
LEFT JOIN LATERAL (
    SELECT id
    FROM provinces
    WHERE ST_Covers(geom, d.geom)
    LIMIT 1
) p ON TRUE;
