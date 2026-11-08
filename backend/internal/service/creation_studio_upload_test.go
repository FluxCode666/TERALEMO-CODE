//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

	"github.com/stretchr/testify/require"
)

// webpFixture is a 32x24 lossless WebP (cwebp -lossless). x/image/webp can only
// decode, so a static fixture is the only way to cover the webp branch.
const webpFixture = "UklGRh4AAABXRUJQVlA4TBEAAAAvH8AFAAdQvCKXp/+BiOh/AAA="

func creationPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 200, G: 100, B: 50, A: 255})
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, img))
	return buffer.Bytes()
}

func creationJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 10, G: 100, B: 200, A: 255})
	var buffer bytes.Buffer
	require.NoError(t, jpeg.Encode(&buffer, img, nil))
	return buffer.Bytes()
}

func creationWebP(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(webpFixture)
	require.NoError(t, err)
	return data
}

var errFakeStorage = errors.New("object storage write failed")

type creationUploadTestStore struct {
	records map[string]*CreationUploadRecord
	active  int64
	saveErr error
}

func newCreationUploadTestStore() *creationUploadTestStore {
	return &creationUploadTestStore{records: map[string]*CreationUploadRecord{}}
}

func (s *creationUploadTestStore) Save(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	return nil
}

func (s *creationUploadTestStore) Get(ctx context.Context, id string) (*ImageTaskRecord, error) {
	return nil, ErrImageTaskNotFound
}

func (s *creationUploadTestStore) SaveCreationUpload(ctx context.Context, rec *CreationUploadRecord) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.records[rec.UploadID] = rec
	return nil
}

func (s *creationUploadTestStore) GetCreationUpload(ctx context.Context, uploadID string) (*CreationUploadRecord, error) {
	record, ok := s.records[uploadID]
	if !ok {
		return nil, ErrCreationUploadNotFound
	}
	return record, nil
}

func (s *creationUploadTestStore) CountActiveCreationUploads(ctx context.Context, userID int64) (int64, error) {
	return s.active, nil
}

type creationUploadTestStorage struct {
	*fakeObjectStorage
	permanent bool
	ttl       time.Duration
}

func (s *creationUploadTestStorage) LinkPolicy() (bool, time.Duration) {
	return s.permanent, s.ttl
}

func newCreationUploadService(t *testing.T) (*ImageTaskService, *creationUploadTestStore, *creationUploadTestStorage) {
	t.Helper()
	store := newCreationUploadTestStore()
	storage := &creationUploadTestStorage{fakeObjectStorage: newFakeObjectStorage(), ttl: 2 * time.Hour}
	uploader := NewImageResultUploader(storage, "images/", 1<<20, nil)
	return NewImageTaskServiceWithUploader(store, uploader, 0, 0), store, storage
}

func TestValidateCreationUploadBytesAcceptsSupportedFormats(t *testing.T) {
	cases := []struct {
		name        string
		data        []byte
		contentType string
		width       int
		height      int
	}{
		{name: "png", data: creationPNG(t, 64, 48), contentType: "image/png", width: 64, height: 48},
		{name: "jpeg", data: creationJPEG(t, 48, 64), contentType: "image/jpeg", width: 48, height: 64},
		{name: "webp", data: creationWebP(t), contentType: "image/webp", width: 32, height: 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := ValidateCreationUploadBytes(tc.data)
			require.NoError(t, err)
			require.Equal(t, tc.contentType, info.ContentType)
			require.Equal(t, tc.width, info.Width)
			require.Equal(t, tc.height, info.Height)
			require.EqualValues(t, len(tc.data), info.SizeBytes)
			require.Len(t, info.SHA256, 64)
		})
	}
}

func TestValidateCreationUploadBytesRejectsUnsupportedFormats(t *testing.T) {
	// GIF bytes behind a .png filename: the declared name and type are ignored.
	gif := append([]byte("GIF89a"), creationPNG(t, 32, 32)...)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"></svg>`)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "gif renamed to png", data: gif},
		{name: "svg renamed to png", data: svg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateCreationUploadBytes(tc.data)
			require.ErrorIs(t, err, ErrCreationUploadUnsupportedType)
		})
	}
}

func TestValidateCreationUploadBytesRejectsOversizedFile(t *testing.T) {
	_, err := ValidateCreationUploadBytes(make([]byte, 11<<20))
	require.ErrorIs(t, err, ErrCreationUploadTooLarge)
	appErr := infraerrors.FromError(err)
	require.NotNil(t, appErr)
	require.Equal(t, "10485760", appErr.Metadata["max_bytes"])
}

func TestValidateCreationUploadBytesRejectsPixelOverflow(t *testing.T) {
	_, err := ValidateCreationUploadBytes(creationPNG(t, 5000, 10))
	require.ErrorIs(t, err, ErrCreationUploadInvalidImage)
	appErr := infraerrors.FromError(err)
	require.NotNil(t, appErr)
	require.Equal(t, "4096", appErr.Metadata["max_side_px"])

	_, err = ValidateCreationUploadBytes(creationPNG(t, 8, 8))
	require.ErrorIs(t, err, ErrCreationUploadInvalidImage, "sides below 16px are rejected")

	_, err = ValidateCreationUploadBytes([]byte("not an image at all"))
	require.ErrorIs(t, err, ErrCreationUploadUnsupportedType)
}

func TestCreationUploadObjectKey(t *testing.T) {
	now := time.Date(2026, 11, 7, 23, 30, 0, 0, time.FixedZone("UTC+8", 8*3600))
	require.Equal(t,
		"creation-uploads/42/20261107/cupl_0123456789abcdef0123456789abcdef.png",
		creationUploadObjectKey(42, "cupl_0123456789abcdef0123456789abcdef", "image/png", now))
	require.Equal(t,
		"creation-uploads/42/20261107/cupl_x.jpg",
		creationUploadObjectKey(42, "cupl_x", "image/jpeg", now))
	require.Equal(t,
		"creation-uploads/42/20261107/cupl_y.webp",
		creationUploadObjectKey(42, "cupl_y", "image/webp", now))
}

func TestCreateCreationUploadStoresObjectAndMetadata(t *testing.T) {
	svc, store, storage := newCreationUploadService(t)
	record, err := svc.CreateCreationUpload(context.Background(), 42, creationPNG(t, 64, 64))
	require.NoError(t, err)
	require.Regexp(t, `^cupl_[0-9a-f]{32}$`, record.UploadID)
	require.Equal(t, int64(42), record.UserID)
	require.Equal(t, "image/png", record.ContentType)
	require.True(t, len(storage.saved) == 1)
	key := "images/" + record.ObjectKey
	require.Contains(t, storage.saved, key)
	require.Regexp(t, `^creation-uploads/42/\d{8}/cupl_[0-9a-f]{32}\.png$`, record.ObjectKey)
	// The upload TTL is min(24h, presign ttl); here the storage says 2h.
	require.EqualValues(t, record.CreatedAt+int64((2*time.Hour).Seconds()), record.ExpiresAt)
	require.Same(t, record, store.records[record.UploadID])
}

func TestCreateCreationUploadUsesPermanentLinkTTL(t *testing.T) {
	svc, _, storage := newCreationUploadService(t)
	storage.permanent = true
	record, err := svc.CreateCreationUpload(context.Background(), 42, creationPNG(t, 32, 32))
	require.NoError(t, err)
	require.EqualValues(t, record.CreatedAt+int64(CreationUploadLinkTTL.Seconds()), record.ExpiresAt)
}

func TestCreateCreationUploadEnforces24hLimit(t *testing.T) {
	svc, store, _ := newCreationUploadService(t)
	store.active = CreationUploadWindowLimit
	_, err := svc.CreateCreationUpload(context.Background(), 42, creationPNG(t, 32, 32))
	require.ErrorIs(t, err, ErrCreationUploadLimitExceeded)
}

func TestCreateCreationUploadRequiresStorage(t *testing.T) {
	svc := NewImageTaskService(newCreationUploadTestStore())
	_, err := svc.CreateCreationUpload(context.Background(), 42, creationPNG(t, 32, 32))
	require.ErrorIs(t, err, ErrCreationStorageDisabled)
}

func TestCreateCreationUploadReportsStorageFailure(t *testing.T) {
	svc, store, _ := newCreationUploadService(t)
	store.saveErr = errFakeStorage
	_, err := svc.CreateCreationUpload(context.Background(), 42, creationPNG(t, 32, 32))
	require.ErrorIs(t, err, ErrCreationUploadStorageFailed)
}

func TestResolveCreationUploadOwnershipAndExpiry(t *testing.T) {
	svc, store, _ := newCreationUploadService(t)
	now := time.Now()
	store.records["cupl_mine"] = &CreationUploadRecord{
		UploadID: "cupl_mine", UserID: 42, ExpiresAt: now.Add(time.Hour).Unix(),
	}
	store.records["cupl_soon"] = &CreationUploadRecord{
		UploadID: "cupl_soon", UserID: 42, ExpiresAt: now.Add(14 * time.Minute).Unix(),
	}
	ctx := context.Background()

	record, err := svc.ResolveCreationUpload(ctx, 42, "cupl_mine", now)
	require.NoError(t, err)
	require.Equal(t, "cupl_mine", record.UploadID)

	_, err = svc.ResolveCreationUpload(ctx, 42, "cupl_soon", now)
	require.ErrorIs(t, err, ErrCreationUploadExpired)

	_, err = svc.ResolveCreationUpload(ctx, 7, "cupl_mine", now)
	require.ErrorIs(t, err, ErrCreationUploadNotFound, "another user's upload must look missing")

	_, err = svc.ResolveCreationUpload(ctx, 42, "cupl_absent", now)
	require.ErrorIs(t, err, ErrCreationUploadNotFound)

	_, err = svc.ResolveCreationUpload(ctx, 42, "  ", now)
	require.ErrorIs(t, err, ErrCreationUploadNotFound)
}
