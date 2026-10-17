//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type batchEnterpriseReaderStub struct {
	member *EnterpriseMemberContext
	err    error
}

func (s batchEnterpriseReaderStub) GetMemberContextByUserID(context.Context, int64) (*EnterpriseMemberContext, error) {
	return s.member, s.err
}

func TestBatchImagesRejectEnterpriseBeforeHold(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot *EnterpriseMemberContext
		reader   batchImageEnterpriseReader
		want     error
	}{
		{"snapshot", &EnterpriseMemberContext{EnterpriseID: 1}, nil, ErrEnterpriseGatewayFeatureUnsupported},
		{"fresh membership", nil, batchEnterpriseReaderStub{member: &EnterpriseMemberContext{EnterpriseID: 1}}, ErrEnterpriseGatewayFeatureUnsupported},
		{"lookup unavailable", nil, batchEnterpriseReaderStub{err: errors.New("database unavailable")}, ErrBillingServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// All job, queue and billing dependencies are nil: rejection must precede their use.
			svc := &BatchImagePublicService{EnterpriseRepo: tc.reader}
			_, err := svc.Submit(context.Background(), BatchImageOwner{UserID: 1, Enterprise: tc.snapshot}, BatchImageSubmitRequest{}, "")
			require.ErrorIs(t, err, tc.want)
		})
	}
}
