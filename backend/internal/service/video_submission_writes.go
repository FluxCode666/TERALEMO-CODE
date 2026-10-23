package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
)

type videoSubmissionWritesKey struct{}
type upstreamWriteAttemptKey struct{}

// VideoSubmissionWrites proves non-delivery only for transports that attest to
// httptrace support. Missing callbacks from an unknown client prove nothing.
type VideoSubmissionWrites struct {
	mu                sync.Mutex
	attempts          []*upstreamWriteAttempt
	responseStatus    atomic.Int32
	rejectionMu       sync.Mutex
	rejectionBody     []byte
	rejectionComplete bool
	rejectionTooLarge bool
}

type upstreamWriteAttempt struct {
	traceSupported atomic.Bool
	mayHaveWritten atomic.Bool
}

func WithVideoSubmissionWrites(ctx context.Context) (context.Context, *VideoSubmissionWrites) {
	writes := &VideoSubmissionWrites{}
	return context.WithValue(ctx, videoSubmissionWritesKey{}, writes), writes
}

// ReceivedResponse distinguishes an actual HTTP rejection from a locally
// synthesized gateway error after an ambiguous transport failure.
func (w *VideoSubmissionWrites) ReceivedResponse() bool { return w.responseStatus.Load() != 0 }

// ReceivedRejection uses the upstream status, never a synthesized 502 caused by
// a transport failure or an invalid/missing task ID in an upstream 2xx response.
func (w *VideoSubmissionWrites) ReceivedRejection() bool { return w.responseStatus.Load() >= 400 }

// RejectionBody returns the complete upstream error, before privacy/error
// rewriting. Oversized, unreadable and partial bodies cannot prove rejection.
func (w *VideoSubmissionWrites) RejectionBody() ([]byte, bool) {
	w.rejectionMu.Lock()
	defer w.rejectionMu.Unlock()
	if !w.ReceivedRejection() || !w.rejectionComplete || w.rejectionTooLarge {
		return nil, false
	}
	return append([]byte(nil), w.rejectionBody...), true
}

type videoRejectionBody struct {
	io.ReadCloser
	writes *VideoSubmissionWrites
	limit  int64
}

func (b *videoRejectionBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	w := b.writes
	w.rejectionMu.Lock()
	defer w.rejectionMu.Unlock()
	if !w.rejectionTooLarge {
		if int64(len(w.rejectionBody))+int64(n) > b.limit {
			w.rejectionBody, w.rejectionTooLarge = nil, true
		} else {
			w.rejectionBody = append(w.rejectionBody, p[:n]...)
		}
	}
	if err == io.EOF {
		w.rejectionComplete = true
	}
	return n, err
}

// DefinitelyNotSent includes local validation failures before any POST attempt.
// GotConn is deliberately conservative: a partial write can precede a failed or
// missing WroteRequest callback, so an acquired connection is already uncertain.
func (w *VideoSubmissionWrites) DefinitelyNotSent() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, attempt := range w.attempts {
		if !attempt.traceSupported.Load() || attempt.mayHaveWritten.Load() {
			return false
		}
	}
	return true
}

// ConfirmHTTPUpstreamWriteTrace is called only by the trusted repository when
// local setup failed before RoundTrip, or the chosen transport supports traces.
func ConfirmHTTPUpstreamWriteTrace(req *http.Request) {
	if req == nil {
		return
	}
	if attempt, ok := req.Context().Value(upstreamWriteAttemptKey{}).(*upstreamWriteAttempt); ok {
		attempt.traceSupported.Store(true)
	}
}

func trackVideoSubmissionWrite(req *http.Request) (*http.Request, *upstreamWriteAttempt) {
	writes, _ := req.Context().Value(videoSubmissionWritesKey{}).(*VideoSubmissionWrites)
	if writes == nil || req.Method != http.MethodPost {
		return req, nil
	}
	attempt := &upstreamWriteAttempt{}
	writes.mu.Lock()
	writes.attempts = append(writes.attempts, attempt)
	writes.mu.Unlock()
	markPossibleWrite := func() { attempt.mayHaveWritten.Store(true) }
	trace := &httptrace.ClientTrace{
		GotConn:              func(httptrace.GotConnInfo) { markPossibleWrite() },
		WroteHeaders:         markPossibleWrite,
		WroteRequest:         func(httptrace.WroteRequestInfo) { markPossibleWrite() },
		GotFirstResponseByte: markPossibleWrite,
	}
	ctx := context.WithValue(req.Context(), upstreamWriteAttemptKey{}, attempt)
	return req.WithContext(httptrace.WithClientTrace(ctx, trace)), attempt
}

// Only the creation POST carries a trace. Seedance draft-validation GETs must
// not turn a later local creation failure into an ambiguous submission.
func (s *OpenAIGatewayService) doVideoUpstreamRequest(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	req, attempt := trackVideoSubmissionWrite(req)
	resp, err := s.httpUpstream.Do(req, proxyURL, accountID, concurrency)
	if attempt != nil && resp != nil {
		attempt.mayHaveWritten.Store(true)
		writes, _ := req.Context().Value(videoSubmissionWritesKey{}).(*VideoSubmissionWrites)
		writes.responseStatus.Store(int32(resp.StatusCode))
		if resp.StatusCode >= 400 && resp.Body != nil {
			resp.Body = &videoRejectionBody{ReadCloser: resp.Body, writes: writes, limit: resolveUpstreamResponseReadLimit(s.cfg)}
		}
	}
	return resp, err
}
