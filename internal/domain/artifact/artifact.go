// Package artifact 定义项目受管 Artifact 的稳定元数据。
package artifact

import (
	"errors"
	"strings"
	"time"
)

// Kind 表示 Artifact 的内容类别。
type Kind string

const (
	// KindImage 表示由用户粘贴或上传的图片。
	KindImage Kind = "IMAGE"
)

// ImageVariant 表示图片的原始或 AI 优化版本。
type ImageVariant string

const (
	// VariantOriginal 保留用户提供的原始图片。
	VariantOriginal ImageVariant = "original"
	// VariantOptimized 用于界面预览和 Agent Context。
	VariantOptimized ImageVariant = "optimized"
)

// Artifact 保存图片两个不可变版本的索引和校验信息。
type Artifact struct {
	ID                    string    `json:"id"`
	Kind                  Kind      `json:"kind"`
	OriginalName          string    `json:"original_name"`
	OriginalMediaType     string    `json:"original_media_type"`
	OriginalRelativePath  string    `json:"-"`
	OriginalSize          int64     `json:"original_size"`
	OriginalSHA256        string    `json:"original_sha256"`
	OptimizedMediaType    string    `json:"optimized_media_type"`
	OptimizedRelativePath string    `json:"-"`
	OptimizedSize         int64     `json:"optimized_size"`
	OptimizedSHA256       string    `json:"optimized_sha256"`
	CreatedAt             time.Time `json:"created_at"`
}

// CreateImageInput 是创建不可变图片 Artifact 所需的两个版本。
type CreateImageInput struct {
	OriginalName       string
	OriginalMediaType  string
	Original           []byte
	OptimizedMediaType string
	Optimized          []byte
}

// Validate 校验图片 Artifact 的必要内容。
func (input CreateImageInput) Validate() error {
	if strings.TrimSpace(input.OriginalName) == "" {
		return errors.New("original image name is required")
	}
	if input.OriginalMediaType == "" || len(input.Original) == 0 {
		return errors.New("original image content is required")
	}
	if input.OptimizedMediaType == "" || len(input.Optimized) == 0 {
		return errors.New("optimized image content is required")
	}
	return nil
}

// UploadedImage 是图片上传接口返回给 Markdown 编辑器的稳定引用。
type UploadedImage struct {
	ID           string `json:"id"`
	Markdown     string `json:"markdown"`
	OriginalURL  string `json:"original_url"`
	OptimizedURL string `json:"optimized_url"`
}
