package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"

	// Register the decoders ValidateCreationUploadBytes sniffs bytes with. The
	// declared Content-Type is ignored on purpose: the bytes decide the format.
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

const (
	// CreationUploadMaxBytes is the per-file size limit (10 MiB).
	CreationUploadMaxBytes int64 = 10 << 20
	// CreationUploadMaxSidePx is the per-side pixel limit.
	CreationUploadMaxSidePx = 4096
	// CreationUploadMinSidePx rejects degenerate images upstream cannot use.
	CreationUploadMinSidePx = 16
	// CreationUploadMaxPixels bounds the total pixel count (16 Mi pixels).
	CreationUploadMaxPixels = 16 << 20
	// CreationUploadWindowLimit bounds the number of live uploads per user.
	CreationUploadWindowLimit = 100
	// CreationUploadLinkTTL is the metadata lifetime cap, whatever the storage
	// link policy says.
	CreationUploadLinkTTL = 24 * time.Hour
	// CreationUploadExpiryMargin is the safety margin kept between "the upload
	// is still valid" and "the upstream pulls the link": 15 minutes.
	CreationUploadExpiryMargin int64 = 900
	// CreationUploadIDPrefix marks creation studio upload identifiers.
	CreationUploadIDPrefix = "cupl_"

	creationUploadKeyPrefix      = "creation_upload:"
	creationUploadObjectDir      = "creation-uploads"
	creationUploadDefaultExt     = ".png"
	creationUploadContentTypePNG = "image/png"
	creationUploadContentTypeJPG = "image/jpeg"
	creationUploadContentTypeWEB = "image/webp"
)

// CreationUploadAccept is the advertised accept list of the upload endpoint.
var CreationUploadAccept = []string{creationUploadContentTypePNG, creationUploadContentTypeJPG, creationUploadContentTypeWEB}

var (
	// ErrCreationStorageDisabled means object storage (the async image switch) is off.
	ErrCreationStorageDisabled = infraerrors.New(http.StatusNotFound, "CREATION_STORAGE_DISABLED", "creation studio uploads are not enabled")
	// ErrCreationUploadNotFound covers "never existed", "expired out of Redis" and
	// "belongs to somebody else" without revealing which one it was.
	ErrCreationUploadNotFound = infraerrors.New(http.StatusNotFound, "CREATION_UPLOAD_NOT_FOUND", "creation upload not found")
	// ErrCreationUploadExpired means the remaining lifetime is under 15 minutes.
	ErrCreationUploadExpired = infraerrors.New(http.StatusGone, "CREATION_UPLOAD_EXPIRED", "creation upload is about to expire; upload the image again")
	// ErrCreationUploadTooLarge means the file exceeds 10 MiB.
	ErrCreationUploadTooLarge = infraerrors.New(http.StatusRequestEntityTooLarge, "CREATION_UPLOAD_TOO_LARGE", "creation upload exceeds the size limit")
	// ErrCreationUploadUnsupportedType means the bytes are not png, jpeg or webp.
	ErrCreationUploadUnsupportedType = infraerrors.New(http.StatusUnsupportedMediaType, "CREATION_UPLOAD_UNSUPPORTED_TYPE", "creation upload must be a png, jpeg or webp image")
	// ErrCreationUploadInvalidImage means the image cannot be decoded or exceeds
	// the pixel limits.
	ErrCreationUploadInvalidImage = infraerrors.New(http.StatusUnprocessableEntity, "CREATION_UPLOAD_INVALID_IMAGE", "creation upload is not a decodable image within the pixel limits")
	// ErrCreationUploadLimitExceeded means 100 live uploads already exist.
	ErrCreationUploadLimitExceeded = infraerrors.New(http.StatusTooManyRequests, "CREATION_UPLOAD_LIMIT_EXCEEDED", "creation upload limit reached; reuse or wait for older uploads to expire")
	// ErrCreationUploadStorageFailed means the object store or Redis write failed.
	ErrCreationUploadStorageFailed = infraerrors.New(http.StatusBadGateway, "CREATION_UPLOAD_STORAGE_FAILED", "failed to store the creation upload")
)

// CreationUploadRecord is the Redis representation of one uploaded reference
// image. ObjectKey and SHA256 never leave the backend.
type CreationUploadRecord struct {
	UploadID    string `json:"upload_id"`
	UserID      int64  `json:"user_id"`
	ObjectKey   string `json:"object_key"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SHA256      string `json:"sha256"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
}

// CreationUpload is the API-safe view returned to the panel.
type CreationUpload struct {
	UploadID    string `json:"upload_id"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
}

// Public strips the internal fields from a stored record.
func (r *CreationUploadRecord) Public() CreationUpload {
	if r == nil {
		return CreationUpload{}
	}
	return CreationUpload{
		UploadID: r.UploadID, URL: r.URL, ContentType: r.ContentType, SizeBytes: r.SizeBytes,
		Width: r.Width, Height: r.Height, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
	}
}

// CreationUploadStore persists upload metadata in Redis. It is implemented by
// the existing image task store and obtained by type assertion, so no new Wire
// provider is introduced.
type CreationUploadStore interface {
	SaveCreationUpload(ctx context.Context, rec *CreationUploadRecord) error
	// GetCreationUpload returns ErrCreationUploadNotFound when the record is absent.
	GetCreationUpload(ctx context.Context, uploadID string) (*CreationUploadRecord, error)
	CountActiveCreationUploads(ctx context.Context, userID int64) (int64, error)
}

// CreationUploadInfo is the validated shape of an uploaded image.
type CreationUploadInfo struct {
	ContentType string
	SizeBytes   int64
	Width       int
	Height      int
	SHA256      string
}

// ValidateCreationUploadBytes applies the U3–U5 rules of the contract: size,
// sniffed content type (client-declared type and filename are ignored) and
// decodable pixel dimensions.
func ValidateCreationUploadBytes(data []byte) (CreationUploadInfo, error) {
	info := CreationUploadInfo{SizeBytes: int64(len(data))}
	if int64(len(data)) > CreationUploadMaxBytes {
		return info, ErrCreationUploadTooLarge.WithMetadata(map[string]string{"max_bytes": fmt.Sprintf("%d", CreationUploadMaxBytes)})
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(head), ";")[0]))
	switch contentType {
	case creationUploadContentTypePNG, creationUploadContentTypeJPG, creationUploadContentTypeWEB:
	default:
		return info, ErrCreationUploadUnsupportedType
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return info, ErrCreationUploadInvalidImage.WithMetadata(map[string]string{"max_side_px": fmt.Sprintf("%d", CreationUploadMaxSidePx)})
	}
	if config.Width < CreationUploadMinSidePx || config.Height < CreationUploadMinSidePx ||
		config.Width > CreationUploadMaxSidePx || config.Height > CreationUploadMaxSidePx ||
		config.Width*config.Height > CreationUploadMaxPixels {
		return info, ErrCreationUploadInvalidImage.WithMetadata(map[string]string{"max_side_px": fmt.Sprintf("%d", CreationUploadMaxSidePx)})
	}
	sum := sha256.Sum256(data)
	info.ContentType = contentType
	info.Width = config.Width
	info.Height = config.Height
	info.SHA256 = hex.EncodeToString(sum[:])
	return info, nil
}

// creationUploadExtension maps a sniffed content type to the stored extension.
func creationUploadExtension(contentType string) string {
	switch contentType {
	case creationUploadContentTypePNG:
		return ".png"
	case creationUploadContentTypeJPG:
		return ".jpg"
	case creationUploadContentTypeWEB:
		return ".webp"
	default:
		return creationUploadDefaultExt
	}
}

// creationUploadObjectKey builds the relative object key
// creation-uploads/<user_id>/<YYYYMMDD(UTC)>/<upload_id><ext>.
func creationUploadObjectKey(userID int64, uploadID, contentType string, now time.Time) string {
	return fmt.Sprintf("%s/%d/%s/%s%s", creationUploadObjectDir, userID, now.UTC().Format("20060102"), uploadID, creationUploadExtension(contentType))
}

// newCreationUploadID returns cupl_ + 32 lowercase hex characters.
func newCreationUploadID() string {
	return CreationUploadIDPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// creationUploadStore exposes the upload metadata store implemented by the
// injected image task store (same pattern as taskCatalog).
func (s *ImageTaskService) creationUploadStore() (CreationUploadStore, error) {
	if s == nil {
		return nil, ErrImageTaskUnavailable
	}
	store, ok := s.store.(CreationUploadStore)
	if !ok {
		return nil, ErrImageTaskUnavailable
	}
	return store, nil
}

// CreateCreationUpload validates the bytes, stores the object and records its
// metadata. The object is written before Redis; a Redis failure leaves an
// orphaned object for the bucket lifecycle rule instead of rolling back.
func (s *ImageTaskService) CreateCreationUpload(ctx context.Context, userID int64, data []byte) (*CreationUploadRecord, error) {
	if s == nil || !s.Enabled() {
		return nil, ErrCreationStorageDisabled
	}
	store, err := s.creationUploadStore()
	if err != nil {
		return nil, ErrCreationStorageDisabled
	}
	info, err := ValidateCreationUploadBytes(data)
	if err != nil {
		return nil, err
	}
	count, err := store.CountActiveCreationUploads(ctx, userID)
	if err != nil {
		return nil, ErrCreationUploadStorageFailed.WithCause(err)
	}
	if count >= CreationUploadWindowLimit {
		return nil, ErrCreationUploadLimitExceeded
	}
	uploader, enabled := s.current()
	if !enabled || uploader == nil {
		return nil, ErrCreationStorageDisabled
	}
	now := time.Now().UTC()
	uploadID := newCreationUploadID()
	objectKey := creationUploadObjectKey(userID, uploadID, info.ContentType, now)
	url, err := uploader.SaveObject(ctx, objectKey, info.ContentType, data)
	if err != nil {
		return nil, ErrCreationUploadStorageFailed.WithCause(err)
	}
	record := &CreationUploadRecord{
		UploadID: uploadID, UserID: userID, ObjectKey: objectKey, URL: url,
		ContentType: info.ContentType, SizeBytes: info.SizeBytes, Width: info.Width, Height: info.Height,
		SHA256: info.SHA256, CreatedAt: now.Unix(), ExpiresAt: now.Add(creationUploadTTL(uploader)).Unix(),
	}
	if err := store.SaveCreationUpload(ctx, record); err != nil {
		return nil, ErrCreationUploadStorageFailed.WithCause(err)
	}
	return record, nil
}

// creationUploadTTL is min(24h, presigned link lifetime); a permanent public
// link still caps the metadata at 24h.
func creationUploadTTL(uploader *ImageResultUploader) time.Duration {
	permanent, ttl := uploader.LinkPolicy()
	if permanent || ttl <= 0 || ttl > CreationUploadLinkTTL {
		return CreationUploadLinkTTL
	}
	return ttl
}

// ResolveCreationUpload loads an upload for submission: it must exist, belong to
// the caller and keep at least 15 minutes of lifetime, because the upstream only
// pulls the link after the task is created.
func (s *ImageTaskService) ResolveCreationUpload(ctx context.Context, userID int64, uploadID string, now time.Time) (*CreationUploadRecord, error) {
	store, err := s.creationUploadStore()
	if err != nil {
		return nil, ErrCreationUploadNotFound
	}
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		return nil, ErrCreationUploadNotFound
	}
	record, err := store.GetCreationUpload(ctx, uploadID)
	if err != nil {
		return nil, ErrCreationUploadNotFound.WithCause(err)
	}
	if record == nil || record.UserID != userID {
		return nil, ErrCreationUploadNotFound
	}
	if record.ExpiresAt-now.Unix() < CreationUploadExpiryMargin {
		return nil, ErrCreationUploadExpired
	}
	return record, nil
}
