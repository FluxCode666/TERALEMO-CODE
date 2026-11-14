package service

import "context"

type creationStudioAPIKeyContextKey struct{}

type creationStudioAPIKeyReference struct {
	userID int64
	keyID  int64
}

// WithCreationStudioAPIKey authorizes an in-process replay for a key whose
// ownership the panel handler has checked. HTTP headers cannot set this value.
func WithCreationStudioAPIKey(ctx context.Context, userID, keyID int64) context.Context {
	return context.WithValue(WithCreationStudioReplay(ctx), creationStudioAPIKeyContextKey{}, creationStudioAPIKeyReference{
		userID: userID,
		keyID:  keyID,
	})
}

// CreationStudioAPIKey returns the identity to reload for an internal replay.
func CreationStudioAPIKey(ctx context.Context) (userID, keyID int64, ok bool) {
	if !IsCreationStudioReplay(ctx) {
		return 0, 0, false
	}
	reference, ok := ctx.Value(creationStudioAPIKeyContextKey{}).(creationStudioAPIKeyReference)
	if !ok || reference.userID <= 0 || reference.keyID <= 0 {
		return 0, 0, false
	}
	return reference.userID, reference.keyID, true
}
