package repository_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)


// This opt-in test must point to a fresh, disposable database. It exercises real
// SQL rather than sqlmock, including the migration and the atomic edit path.
func TestMassEmailReviewPostgresRepository(t *testing.T) {
	dsn := os.Getenv("MASS_EMAIL_REVIEW_TEST_DSN")
	if dsn == "" {
		t.Skip("set MASS_EMAIL_REVIEW_TEST_DSN to a disposable local PostgreSQL database")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	migration, err := os.ReadFile("../../migrations/266_mass_email_campaigns.sql")
	require.NoError(t, err)
	// The feature migration must also work without any pre-existing user or
	// enterprise tables. Rollback removes the isolated schema and both tables.
	emptyTx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = emptyTx.Rollback() })
	_, err = emptyTx.ExecContext(ctx, `CREATE SCHEMA mass_email_review_empty; SET LOCAL search_path TO mass_email_review_empty`)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = emptyTx.ExecContext(ctx, string(migration))
		require.NoError(t, err, "feature migration must work in an empty schema and be repeatable")
	}
	require.NoError(t, emptyTx.Rollback())
	// Only the selected user display fields are needed by this feature.
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users (id BIGINT PRIMARY KEY, email TEXT, username TEXT, deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(migration))
		if err != nil {
			_ = tx.Rollback()
		}
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := repository.NewMassEmailRepository(client, db)
	svc := service.NewMassEmailService(repo, nil, nil, nil)
	campaign, err := svc.Create(ctx, 7, service.MassEmailCreateInput{
		Subject: "SQL create", BodyHTML: "<p>Hello</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{ManualEmails: []string{"Alice@Example.com,alice@example.com", "bob@example.com"}},
	})
	require.NoError(t, err, "real SQL must insert the same number of values as target columns")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM mass_email_campaign WHERE id=$1", campaign.ID)
	})
	require.Equal(t, 2, campaign.TotalCount)
	claimed, err := repo.ClaimPendingRecipients(ctx, campaign.ID, 8, domain.MassEmailClaimLease, time.Now())
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	secondClaim, err := repo.ClaimPendingRecipients(ctx, campaign.ID, 8, domain.MassEmailClaimLease, time.Now())
	require.NoError(t, err)
	require.Empty(t, secondClaim, "a second claimant must not take active leases")
	recovered, err := repo.ClaimPendingRecipients(ctx, campaign.ID, 8, domain.MassEmailClaimLease, time.Now().Add(domain.MassEmailClaimLease+time.Second))
	require.NoError(t, err)
	require.Len(t, recovered, 2, "after a restart the expired leases must become claimable")
	for _, row := range recovered {
		require.Equal(t, 2, row.Attempts)
	}
	_, err = repo.ReleaseClaimedRecipients(ctx, campaign.ID, []int64{claimed[0].ID, claimed[1].ID})
	require.NoError(t, err)
	updated, err := svc.Update(ctx, campaign.ID, service.MassEmailUpdateInput{
		Subject: "SQL atomic edit", BodyHTML: "<p>Changed</p>",
		RecipientFilter: domain.MassEmailRecipientFilter{ManualEmails: []string{"new@example.com"}},
	})
	require.NoError(t, err, "recipient_filter JSON must be converted to a SQL driver value")
	require.Equal(t, "SQL atomic edit", updated.Subject)
	require.Equal(t, 1, updated.TotalCount)
	// A failed recipient insert must roll back the content update and deletion
	// in the same transaction. A token beyond the schema limit forces that path.
	invalidUpdate := *updated
	invalidUpdate.Subject = "must roll back"
	_, err = repo.UpdateCampaignWithRecipients(ctx, &invalidUpdate, []domain.MassEmailRecipient{{
		Email: "rollback@example.com", TrackingToken: strings.Repeat("x", 33),
	}})
	require.Error(t, err)
	afterRollback, err := repo.GetCampaign(ctx, campaign.ID)
	require.NoError(t, err)
	require.Equal(t, "SQL atomic edit", afterRollback.Subject)
	claimed, err = repo.ClaimPendingRecipients(ctx, campaign.ID, 8, domain.MassEmailClaimLease, time.Now())
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "new@example.com", claimed[0].Email)
	require.NoError(t, repo.MarkRecipientSent(ctx, campaign.ID, claimed[0].ID, time.Now()))
	svc.RecordOpen(ctx, claimed[0].TrackingToken)
	svc.RecordOpen(ctx, claimed[0].TrackingToken)
	detail, err := svc.Get(ctx, campaign.ID)
	require.NoError(t, err)
	require.Equal(t, 1, detail.Stats.Sent)
	require.Equal(t, 1, detail.Stats.Opened)
	recipient, err := repo.GetRecipient(ctx, campaign.ID, claimed[0].ID)
	require.NoError(t, err)
	require.Equal(t, 2, recipient.OpenCount)
	// An edit racing with a send transition must leave recipients untouched.
	_, err = repo.TransitionCampaignStatus(ctx, campaign.ID, []string{"draft"}, "sending", time.Now())
	require.NoError(t, err)
	deleted, err := repo.DeleteCampaignIfNotSending(ctx, campaign.ID)
	require.NoError(t, err)
	require.False(t, deleted, "the SQL delete must refuse a campaign that has started sending")
	updated.Status = "draft"
	ok, err := repo.UpdateCampaignWithRecipients(ctx, updated, []domain.MassEmailRecipient{{Email: "wrong@example.com", TrackingToken: strings.Repeat("f", 32)}})
	require.NoError(t, err)
	require.False(t, ok)
	stillSent, err := repo.GetRecipient(ctx, campaign.ID, claimed[0].ID)
	require.NoError(t, err)
	require.Equal(t, "sent", stillSent.Status)
}
