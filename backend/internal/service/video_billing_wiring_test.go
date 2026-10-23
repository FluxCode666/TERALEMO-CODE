//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type videoWiringReservationRepo struct {
	VideoBillingReservationRepository
	started chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func (r *videoWiringReservationRepo) ClaimDue(ctx context.Context, _ string, _ int, _ time.Duration) ([]*VideoBillingReservation, error) {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	close(r.stopped)
	return nil, ctx.Err()
}

func TestProvideOpenAIGatewayServiceStartsDurableVideoReconciler(t *testing.T) {
	repo := &videoWiringReservationRepo{started: make(chan struct{}), stopped: make(chan struct{})}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := ProvideOpenAIGatewayService(nil, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, repo)
	t.Cleanup(svc.StopVideoBillingReconciler)
	require.True(t, svc.HasDurableVideoBilling())
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("production provider did not start reservation reconciliation")
	}
	svc.StopVideoBillingReconciler()
	select {
	case <-repo.stopped:
	default:
		t.Fatal("reconciliation must finish before application cleanup closes its dependencies")
	}
}
