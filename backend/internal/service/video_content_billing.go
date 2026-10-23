package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The durable reservation owns routing identity after acceptance. A changed
// group or an expired Redis binding must not make an owned task undownloadable.
func (s *OpenAIGatewayService) resolveVideoContentAccount(ctx context.Context, protocol string, task *AsyncTaskRecord) (*Account, error) {
	if task == nil || s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("task account is unavailable")
	}
	if protocol == "" && strings.HasPrefix(task.RequestID, "minimax-file:") {
		protocol = VideoProtocolMiniMax
	}
	var accountID int64
	reservation, err := s.FindVideoTaskReservation(ctx, protocol, task.RequestID)
	if err == nil {
		if reservation.UserID != task.UserID || reservation.APIKeyID != task.APIKeyID {
			return nil, fmt.Errorf("task account is unavailable")
		}
		accountID = reservation.AccountID
	} else if errors.Is(err, ErrVideoReservationNotFound) {
		accountID, err = s.ResolveGrokMediaVideoRequestAccount(ctx, task.GroupID, task.RequestID, task.UserID, task.APIKeyID)
	}
	if err != nil || accountID <= 0 {
		return nil, fmt.Errorf("task account is unavailable")
	}
	return s.accountRepo.GetByID(ctx, accountID)
}

// File-only downloads have no native status result. Resolve the durable task
// behind the file and observe its terminal state before serving bytes. A task
// already captured by the normal native download path needs no second poll.
func (s *OpenAIGatewayService) observeReservedVideoDownload(ctx context.Context, protocol, taskID string) error {
	reservation, err := s.FindVideoTaskReservation(ctx, protocol, taskID)
	if errors.Is(err, ErrVideoReservationNotFound) {
		return nil // rolling-upgrade task; the existing legacy path still applies.
	}
	if err != nil {
		return err
	}
	if reservation.State == VideoReservationSettled || reservation.State == VideoReservationReleased {
		return nil
	}
	result, err := s.PollReservedVideoTask(ctx, reservation)
	if err != nil {
		return err
	}
	_, err = s.ObserveVideoTaskBilling(ctx, reservation.Protocol, reservation.TaskID, result, false)
	return err
}
