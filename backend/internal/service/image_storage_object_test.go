//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeObjectStorage struct {
	saved       map[string][]byte
	contentType map[string]string
	saveErr     error
	readErr     error
}

func newFakeObjectStorage() *fakeObjectStorage {
	return &fakeObjectStorage{saved: map[string][]byte{}, contentType: map[string]string{}}
}

func (f *fakeObjectStorage) Save(ctx context.Context, key, contentType string, data []byte) (string, error) {
	if f.saveErr != nil {
		return "", f.saveErr
	}
	f.saved[key] = append([]byte(nil), data...)
	f.contentType[key] = contentType
	return "https://cdn.example/" + key, nil
}

type fakeReadingImageStorage struct {
	*fakeObjectStorage
	permanent bool
	ttl       time.Duration
}

func (f *fakeReadingImageStorage) Read(ctx context.Context, key string, maxBytes int64) ([]byte, string, error) {
	if f.readErr != nil {
		return nil, "", f.readErr
	}
	data, ok := f.saved[key]
	if !ok {
		return nil, "", errors.New("object not found")
	}
	if int64(len(data)) > maxBytes {
		return nil, "", errors.New("object exceeds max bytes")
	}
	return append([]byte(nil), data...), f.contentType[key], nil
}

func (f *fakeReadingImageStorage) LinkPolicy() (bool, time.Duration) {
	return f.permanent, f.ttl
}

func TestImageResultUploaderSaveObjectPrefixesKey(t *testing.T) {
	storage := newFakeObjectStorage()
	uploader := NewImageResultUploader(storage, "images/", 1<<20, nil)
	url, err := uploader.SaveObject(context.Background(), "creation-uploads/42/20261107/cupl_x.png", "image/png", []byte{1, 2, 3})
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/images/creation-uploads/42/20261107/cupl_x.png", url)
	require.Contains(t, storage.saved, "images/creation-uploads/42/20261107/cupl_x.png")
	require.Equal(t, "image/png", storage.contentType["images/creation-uploads/42/20261107/cupl_x.png"])
}

func TestImageResultUploaderSaveObjectRequiresStorage(t *testing.T) {
	var uploader *ImageResultUploader
	_, err := uploader.SaveObject(context.Background(), "k", "image/png", nil)
	require.ErrorIs(t, err, ErrImageStorageUnavailable)

	_, err = NewImageResultUploader(nil, "", 0, nil).SaveObject(context.Background(), "k", "image/png", nil)
	require.ErrorIs(t, err, ErrImageStorageUnavailable)

	_, err = NewImageResultUploader(newFakeObjectStorage(), "", 0, nil).SaveObject(context.Background(), "  ", "image/png", nil)
	require.Error(t, err)
}

func TestImageResultUploaderReadObject(t *testing.T) {
	storage := &fakeReadingImageStorage{fakeObjectStorage: newFakeObjectStorage(), ttl: 3 * time.Hour}
	uploader := NewImageResultUploader(storage, "images/", 1<<20, nil)
	_, err := uploader.SaveObject(context.Background(), "creation-uploads/42/a.png", "image/png", []byte("payload"))
	require.NoError(t, err)

	data, contentType, err := uploader.ReadObject(context.Background(), "creation-uploads/42/a.png", 16)
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), data)
	require.Equal(t, "image/png", contentType)

	_, _, err = uploader.ReadObject(context.Background(), "creation-uploads/42/a.png", 3)
	require.Error(t, err, "reading more than maxBytes must fail")
}

func TestImageResultUploaderReadObjectUnsupported(t *testing.T) {
	uploader := NewImageResultUploader(newFakeObjectStorage(), "images/", 1<<20, nil)
	_, _, err := uploader.ReadObject(context.Background(), "creation-uploads/42/a.png", 16)
	require.ErrorIs(t, err, ErrImageStorageReadUnsupported)
}

func TestImageResultUploaderLinkPolicy(t *testing.T) {
	permanent, ttl := NewImageResultUploader(newFakeObjectStorage(), "", 0, nil).LinkPolicy()
	require.False(t, permanent)
	require.Equal(t, defaultImageStorageLinkTTL, ttl)

	storage := &fakeReadingImageStorage{fakeObjectStorage: newFakeObjectStorage(), permanent: true, ttl: 2 * time.Hour}
	permanent, ttl = NewImageResultUploader(storage, "", 0, nil).LinkPolicy()
	require.True(t, permanent)
	require.Equal(t, 2*time.Hour, ttl)

	var nilUploader *ImageResultUploader
	permanent, ttl = nilUploader.LinkPolicy()
	require.False(t, permanent)
	require.Equal(t, defaultImageStorageLinkTTL, ttl)
}
