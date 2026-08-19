package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/light-speak/aitodos/internal/domain/artifact"
	"github.com/light-speak/aitodos/internal/storage"
)

const (
	maxArtifactRequestBytes = 20 << 20
	maxOriginalImageBytes   = 12 << 20
	maxOptimizedImageBytes  = 5 << 20
	maxOptimizedImageEdge   = 2000
)

type artifactHandler struct {
	store *storage.ArtifactStore
}

// RegisterArtifactRoutes 注册图片 Artifact 上传和只读内容端点。
func RegisterArtifactRoutes(mux *http.ServeMux, store *storage.ArtifactStore) {
	handler := &artifactHandler{store: store}
	mux.HandleFunc("POST /api/artifacts/images", handler.uploadImage)
	mux.HandleFunc("GET /api/artifacts/{artifactID}/content", handler.readImage)
}

func (handler *artifactHandler) uploadImage(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, maxArtifactRequestBytes)
	if err := request.ParseMultipartForm(maxArtifactRequestBytes); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_IMAGE_UPLOAD", "图片上传内容无效或超过 20 MB")
		return
	}
	if request.MultipartForm != nil {
		defer request.MultipartForm.RemoveAll()
	}
	original, originalHeader, err := readUploadPart(request, "original", maxOriginalImageBytes)
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_ORIGINAL_IMAGE", err.Error())
		return
	}
	optimized, _, err := readUploadPart(request, "optimized", maxOptimizedImageBytes)
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_OPTIMIZED_IMAGE", err.Error())
		return
	}
	originalType, err := validateOriginalImage(original)
	if err != nil {
		writeError(response, http.StatusBadRequest, "UNSUPPORTED_IMAGE", err.Error())
		return
	}
	optimizedType, err := validateOptimizedImage(optimized)
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_OPTIMIZED_IMAGE", err.Error())
		return
	}
	created, err := handler.store.CreateImage(request.Context(), artifact.CreateImageInput{
		OriginalName: filepath.Base(originalHeader.Filename), OriginalMediaType: originalType, Original: original,
		OptimizedMediaType: optimizedType, Optimized: optimized,
	})
	if err != nil {
		writeError(response, http.StatusInternalServerError, "IMAGE_STORE_FAILED", "保存图片失败")
		return
	}
	escapedID := url.PathEscape(created.ID)
	writeJSON(response, http.StatusCreated, artifact.UploadedImage{
		ID: created.ID, Markdown: "![图片](artifact://" + created.ID + ")",
		OriginalURL:  "/api/artifacts/" + escapedID + "/content?variant=original",
		OptimizedURL: "/api/artifacts/" + escapedID + "/content?variant=optimized",
	})
}

func (handler *artifactHandler) readImage(response http.ResponseWriter, request *http.Request) {
	variant := artifact.ImageVariant(request.URL.Query().Get("variant"))
	if variant == "" {
		variant = artifact.VariantOptimized
	}
	content, mediaType, err := handler.store.ReadImageVariant(request.Context(), request.PathValue("artifactID"), variant)
	if errors.Is(err, storage.ErrArtifactNotFound) {
		writeError(response, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "图片不存在")
		return
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_IMAGE_VARIANT", "图片版本无效")
		return
	}
	response.Header().Set("Content-Type", mediaType)
	response.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(content)
}

func readUploadPart(request *http.Request, field string, limit int64) ([]byte, *multipart.FileHeader, error) {
	file, header, err := request.FormFile(field)
	if err != nil {
		return nil, nil, fmt.Errorf("缺少 %s 图片", field)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("读取 %s 图片失败", field)
	}
	if len(content) == 0 || int64(len(content)) > limit {
		return nil, nil, fmt.Errorf("%s 图片为空或超过大小限制", field)
	}
	return content, header, nil
}

func validateOriginalImage(content []byte) (string, error) {
	mediaType := detectImageType(content)
	if mediaType == "image/webp" {
		return mediaType, nil
	}
	if mediaType != "image/png" && mediaType != "image/jpeg" {
		return "", errors.New("仅支持 PNG、JPEG 或 WebP 图片")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(content)); err != nil {
		return "", errors.New("图片内容损坏")
	}
	return mediaType, nil
}

func validateOptimizedImage(content []byte) (string, error) {
	mediaType := detectImageType(content)
	if mediaType != "image/png" && mediaType != "image/jpeg" {
		return "", errors.New("优化图片必须是 PNG 或 JPEG")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return "", errors.New("优化图片内容损坏")
	}
	if config.Width > maxOptimizedImageEdge || config.Height > maxOptimizedImageEdge {
		return "", fmt.Errorf("优化图片最长边不能超过 %d px", maxOptimizedImageEdge)
	}
	return mediaType, nil
}

func detectImageType(content []byte) string {
	mediaType := http.DetectContentType(content)
	if len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP" {
		return "image/webp"
	}
	return mediaType
}
