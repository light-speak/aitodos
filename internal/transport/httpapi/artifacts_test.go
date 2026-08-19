package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/artifact"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestArtifactRoutesUploadAndReadImage(t *testing.T) {
	database := storageTestDatabase(t)
	store := storage.NewArtifactStore(database, filepath.Join(t.TempDir(), "artifacts"))
	mux := http.NewServeMux()
	RegisterArtifactRoutes(mux, store)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	imageContent := testPNG(t)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writeMultipartFile(t, writer, "original", "screen.png", imageContent)
	writeMultipartFile(t, writer, "optimized", "screen-optimized.png", imageContent)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/artifacts/images", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", response.StatusCode)
	}
	var uploaded artifact.UploadedImage
	if err := json.NewDecoder(response.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Markdown != "![图片](artifact://"+uploaded.ID+")" {
		t.Fatalf("markdown = %q", uploaded.Markdown)
	}

	contentResponse, err := server.Client().Get(server.URL + "/api/artifacts/" + uploaded.ID + "/content?variant=optimized")
	if err != nil {
		t.Fatal(err)
	}
	defer contentResponse.Body.Close()
	if contentResponse.StatusCode != http.StatusOK || contentResponse.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("content response = %d, %q", contentResponse.StatusCode, contentResponse.Header.Get("Content-Type"))
	}
}

func TestArtifactRoutesRejectNonImageUpload(t *testing.T) {
	database := storageTestDatabase(t)
	mux := http.NewServeMux()
	RegisterArtifactRoutes(mux, storage.NewArtifactStore(database, filepath.Join(t.TempDir(), "artifacts")))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writeMultipartFile(t, writer, "original", "notes.txt", []byte("not an image"))
	writeMultipartFile(t, writer, "optimized", "notes.txt", []byte("not an image"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/artifacts/images", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("upload status = %d, want 400", response.StatusCode)
	}
}

func storageTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), storage.ProjectMetadata{
		InstanceID: "artifact-http-test", Name: "artifact-test", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func writeMultipartFile(t *testing.T, writer *multipart.Writer, field, name string, content []byte) {
	t.Helper()
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.Black)
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
