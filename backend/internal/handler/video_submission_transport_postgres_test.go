//go:build unit

package handler

import (
	"context"
	"database/sql"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Opt into a fresh, disposable database, distinct from repository test fixtures.
// Both HTTPUpstream and the monetary ledger are real in this regression.
func TestVideoSubmissionTransportPostgresNoFrozenResidual(t *testing.T) {
	dsn := os.Getenv("VIDEO_SUBMISSION_TEST_DSN")
	if dsn == "" {
		t.Skip("set VIDEO_SUBMISSION_TEST_DSN to a fresh dedicated PostgreSQL test database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repository.ApplyMigrations(context.Background(), db))
	for _, query := range []string{
		`INSERT INTO users(id,email,password_hash,balance) VALUES(10,'unsent@video.test','local-test',1)`,
		`INSERT INTO groups(id,name,platform) VALUES(24,'unsent-video','video')`,
		`INSERT INTO accounts(id,name,platform,type) VALUES(1,'unsent-video','video','apikey')`,
		`INSERT INTO api_keys(id,user_id,key,name,group_id) VALUES(20,10,'sk-local-unsent-test','unsent-video',24)`,
	} {
		_, err := db.Exec(query)
		require.NoError(t, err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("untrusted TLS must prevent the HTTP request")
	}))
	t.Cleanup(srv.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	refused := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	for _, protocol := range []string{service.VideoProtocolSeedance, service.VideoProtocolGemini, "minimax-v1", "minimax-v2", "grok"} {
		for _, fault := range []string{"tls", "refused", "dns"} {
			t.Run(protocol+"/"+fault, func(t *testing.T) {
				origin := srv.URL
				if fault == "refused" {
					origin = refused
				}
				if fault == "dns" {
					origin = "https://video-unsent-test.invalid"
				}
				h, slots, _ := transportVideoHandler(t, protocol, transportVideoBase(protocol, origin), nil)
				h.gatewayService.SetVideoBillingReservationRepository(repository.NewVideoBillingReservationRepository(db))
				w := submitTransportVideo(t, h, protocol)
				require.Equal(t, 502, w.Code, w.Body.String())
				var balance, frozen float64
				require.NoError(t, db.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=10`).Scan(&balance, &frozen))
				require.Equal(t, 1.0, balance)
				require.Zero(t, frozen)
				var active, unreleased, count int
				require.NoError(t, db.QueryRow(`SELECT count(*) FILTER (WHERE state IN ('preparing','accepted','manual')),
 count(*) FILTER (WHERE state <> 'released' OR hold_amount <> 0.2 OR last_error <> 'upstream request not sent'), count(*)
 FROM video_billing_reservations WHERE user_id=10`).Scan(&active, &unreleased, &count))
				require.Zero(t, active)
				require.Zero(t, unreleased)
				require.Positive(t, count, "a real SQL hold and release must have occurred")
				t.Logf("HTTP=%d balance=$%.2f frozen=$%.2f active=%d released=%d", w.Code, balance, frozen, active, count)
				slots.assertReleased(t)
			})
		}
	}
	t.Run("metaso-h3-402-tracking-id-releases-096", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "17")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = io.WriteString(w, metasoH3InsufficientBalanceBody)
		}))
		t.Cleanup(upstream.Close)
		h, slots, _ := transportVideoHandler(t, "minimax-v2", upstream.URL+"/v2", nil)
		h.gatewayService.SetVideoBillingReservationRepository(repository.NewVideoBillingReservationRepository(db))
		w := submitTransportVideoAtPrice(t, h, "minimax-v2", .96)
		require.Equal(t, 402, w.Code, w.Body.String())
		var balance, frozen float64
		require.NoError(t, db.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=10`).Scan(&balance, &frozen))
		require.Equal(t, 1.0, balance)
		require.Zero(t, frozen)
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM video_billing_reservations WHERE user_id=10 AND hold_amount=.96
 AND state='released' AND last_error='upstream explicitly rejected creation'`).Scan(&count))
		require.Equal(t, 1, count)
		var active int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM video_billing_reservations WHERE user_id=10 AND state IN ('preparing','accepted','manual')`).Scan(&active))
		require.Zero(t, active)
		t.Logf("metaso H3 HTTP=%d released=$0.96 balance=$%.2f frozen=$%.2f active=%d", w.Code, balance, frozen, active)
		slots.assertReleased(t)
	})
	t.Run("normal-h3-settles-once", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v2/video_generation", r.URL.Path)
			_, _ = io.WriteString(w, `{"task_id":"postgres-normal-h3"}`)
		}))
		t.Cleanup(upstream.Close)
		h, slots, _ := transportVideoHandler(t, "minimax-v2", upstream.URL+"/v2", nil)
		h.gatewayService.SetVideoBillingReservationRepository(repository.NewVideoBillingReservationRepository(db))
		w := submitTransportVideo(t, h, "minimax-v2")
		require.Equal(t, 200, w.Code, w.Body.String())
		var balance, frozen float64
		require.NoError(t, db.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=10`).Scan(&balance, &frozen))
		require.Equal(t, .8, balance)
		require.Equal(t, .2, frozen)
		for i := 0; i < 2; i++ {
			handled, err := h.gatewayService.ObserveVideoTaskBilling(context.Background(), service.VideoProtocolMiniMax, "minimax:postgres-normal-h3",
				&service.OpenAIForwardResult{AsyncTaskStatus: "completed", VideoCount: 1, VideoDurationSeconds: 6}, false)
			require.NoError(t, err)
			require.True(t, handled)
		}
		require.NoError(t, db.QueryRow(`SELECT balance,frozen_balance FROM users WHERE id=10`).Scan(&balance, &frozen))
		require.Equal(t, .8, balance)
		require.Zero(t, frozen)
		var count int
		var cost float64
		var inbound, outbound string
		require.NoError(t, db.QueryRow(`SELECT count(*),sum(actual_cost),min(inbound_endpoint),min(upstream_endpoint) FROM usage_logs WHERE user_id=10`).Scan(&count, &cost, &inbound, &outbound))
		require.Equal(t, 1, count)
		require.Equal(t, .2, cost)
		require.Equal(t, "/v2/video_generation", inbound)
		require.Equal(t, "/v2/video_generation", outbound)
		t.Logf("normal H3: balance=$%.2f frozen=$%.2f usage_count=%d cost=$%.2f endpoint=%s", balance, frozen, count, cost, inbound)
		slots.assertReleased(t)
	})
}
