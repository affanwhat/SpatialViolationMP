package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxUploadBytes = 10 << 20

type application struct {
	db        *pgxpool.Pool
	uploadDir string
}

type featureCollection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

type feature struct {
	Type       string          `json:"type"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties any             `json:"properties"`
}

type violationProperties struct {
	ID        int64     `json:"id"`
	Province  *string   `json:"province"`
	ImagePath *string   `json:"image_path"`
	Status    string    `json:"status"`
	IsDummy   bool      `json:"is_dummy"`
	CreatedAt time.Time `json:"created_at"`
}

func main() {
	databaseURL := envOrDefault("DATABASE_URL", "postgres://violation_map:violation_map@localhost:5432/violation_map?sslmode=disable")
	uploadDir := envOrDefault("UPLOAD_DIR", "uploads")
	port := envOrDefault("PORT", "8080")

	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		log.Fatal(err)
	}

	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.Ping(ctx); err != nil {
		log.Fatalf("connect to database: %v", err)
	}

	app := &application{db: db, uploadDir: uploadDir}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/provinces", app.getProvinces)
	mux.HandleFunc("GET /api/violations", app.getViolations)
	mux.HandleFunc("POST /api/violations", app.createViolation)
	mux.HandleFunc("GET /api/admin/violations", app.getPendingViolations)
	mux.HandleFunc("PATCH /api/admin/violations/{id}", app.updateViolationStatus)
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))
	mux.Handle("/", http.FileServer(http.Dir("static")))

	log.Printf("violation map listening on http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, logRequests(mux)))
}

func (app *application) getProvinces(w http.ResponseWriter, r *http.Request) {
	rows, err := app.db.Query(r.Context(), `
		SELECT id, name, ST_AsGeoJSON(geom)
		FROM provinces
		ORDER BY name`)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()

	collection := featureCollection{Type: "FeatureCollection", Features: []feature{}}
	for rows.Next() {
		var id int
		var name string
		var geometry []byte
		if err := rows.Scan(&id, &name, &geometry); err != nil {
			serverError(w, err)
			return
		}
		collection.Features = append(collection.Features, feature{
			Type:       "Feature",
			Geometry:   geometry,
			Properties: map[string]any{"id": id, "name": name},
		})
	}
	writeJSON(w, http.StatusOK, collection)
}

func (app *application) getViolations(w http.ResponseWriter, r *http.Request) {
	calibrated := r.URL.Query().Get("calibrated") == "true"
	where := "v.is_dummy = TRUE"
	if calibrated {
		where = "(v.status = 'verified' OR v.is_dummy = TRUE)"
	}
	app.writeViolations(w, r, where)
}

func (app *application) getPendingViolations(w http.ResponseWriter, r *http.Request) {
	app.writeViolations(w, r, "v.status = 'unverified'")
}

func (app *application) writeViolations(w http.ResponseWriter, r *http.Request, where string) {
	rows, err := app.db.Query(r.Context(), `
		SELECT v.id, p.name, v.image_path, v.status, v.is_dummy, v.created_at,
		       ST_AsGeoJSON(v.geom)
		FROM violations v
		LEFT JOIN provinces p ON p.id = v.province_id
		WHERE `+where+`
		ORDER BY v.created_at DESC`)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()

	collection := featureCollection{Type: "FeatureCollection", Features: []feature{}}
	for rows.Next() {
		var properties violationProperties
		var geometry []byte
		if err := rows.Scan(
			&properties.ID,
			&properties.Province,
			&properties.ImagePath,
			&properties.Status,
			&properties.IsDummy,
			&properties.CreatedAt,
			&geometry,
		); err != nil {
			serverError(w, err)
			return
		}
		collection.Features = append(collection.Features, feature{
			Type:       "Feature",
			Geometry:   geometry,
			Properties: properties,
		})
	}
	writeJSON(w, http.StatusOK, collection)
}

func (app *application) createViolation(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		clientError(w, http.StatusBadRequest, "image upload must be smaller than 10 MB")
		return
	}

	lat, err := parseCoordinate(r.FormValue("lat"), -90, 90)
	if err != nil {
		clientError(w, http.StatusBadRequest, "invalid latitude")
		return
	}
	lng, err := parseCoordinate(r.FormValue("lng"), -180, 180)
	if err != nil {
		clientError(w, http.StatusBadRequest, "invalid longitude")
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		clientError(w, http.StatusBadRequest, "image is required")
		return
	}
	defer file.Close()

	imagePath, err := app.saveImage(file, header)
	if err != nil {
		clientError(w, http.StatusBadRequest, err.Error())
		return
	}

	var id int64
	err = app.db.QueryRow(r.Context(), `
		INSERT INTO violations (province_id, geom, image_path, status, is_dummy)
		VALUES (
			(SELECT id FROM provinces
			 WHERE ST_Covers(geom, ST_SetSRID(ST_MakePoint($1, $2), 4326))
			 LIMIT 1),
			ST_SetSRID(ST_MakePoint($1, $2), 4326),
			$3,
			'unverified',
			FALSE
		)
		RETURNING id`,
		lng, lat, imagePath,
	).Scan(&id)
	if err != nil {
		_ = os.Remove(filepath.Join(app.uploadDir, filepath.Base(imagePath)))
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "unverified"})
}

func (app *application) updateViolationStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		clientError(w, http.StatusBadRequest, "invalid violation id")
		return
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		clientError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Status != "verified" && body.Status != "rejected" {
		clientError(w, http.StatusBadRequest, "status must be verified or rejected")
		return
	}

	tag, err := app.db.Exec(r.Context(), `
		UPDATE violations SET status = $1
		WHERE id = $2 AND status = 'unverified'`,
		body.Status, id,
	)
	if err != nil {
		serverError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		clientError(w, http.StatusNotFound, "unverified violation not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": body.Status})
}

func (app *application) saveImage(file multipart.File, _ *multipart.FileHeader) (string, error) {
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read image: %w", err)
	}
	contentType := http.DetectContentType(buffer[:n])
	extensions := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
	}
	extension, ok := extensions[contentType]
	if !ok {
		return "", fmt.Errorf("image must be JPEG, PNG, or WebP")
	}

	name, err := randomName(extension)
	if err != nil {
		return "", fmt.Errorf("generate image name: %w", err)
	}
	destination, err := os.OpenFile(filepath.Join(app.uploadDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("create image file: %w", err)
	}
	defer destination.Close()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(filepath.Join(app.uploadDir, name))
		}
	}()

	if _, err := destination.Write(buffer[:n]); err != nil {
		return "", fmt.Errorf("save image: %w", err)
	}
	if _, err := io.Copy(destination, file); err != nil {
		return "", fmt.Errorf("save image: %w", err)
	}
	cleanup = false
	return "/uploads/" + name, nil
}

func parseCoordinate(value string, min, max float64) (float64, error) {
	coordinate, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(coordinate) || math.IsInf(coordinate, 0) || coordinate < min || coordinate > max {
		return 0, errors.New("invalid coordinate")
	}
	return coordinate, nil
}

func randomName(extension string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes) + extension, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func clientError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	clientError(w, http.StatusInternalServerError, "internal server error")
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.RequestURI(), time.Since(start).Round(time.Millisecond))
	})
}
