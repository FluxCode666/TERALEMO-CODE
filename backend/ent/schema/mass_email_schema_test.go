package schema

import (
	"testing"

	"entgo.io/ent/entc/load"
	"github.com/stretchr/testify/require"
)

// TestMassEmailSchemas 校验邮件群发的两张表在 ent schema 里字段齐全、追踪 token 唯一。
func TestMassEmailSchemas(t *testing.T) {
	spec, err := (&load.Config{Path: "."}).Load()
	require.NoError(t, err)

	schemas := map[string]*load.Schema{}
	for _, schema := range spec.Schemas {
		schemas[schema.Name] = schema
	}

	campaign := requireSchema(t, schemas, "MassEmailCampaign")
	requireSchemaFields(t, campaign,
		"subject",
		"body_html",
		"recipient_filter",
		"status",
		"scheduled_at",
		"total_count",
		"sent_count",
		"failed_count",
		"opened_count",
		"created_by",
		"started_at",
		"finished_at",
		"last_error",
		"created_at",
		"updated_at",
	)

	recipient := requireSchema(t, schemas, "MassEmailRecipient")
	requireSchemaFields(t, recipient,
		"campaign_id",
		"email",
		"name",
		"user_id",
		"enterprise_id",
		"status",
		"error",
		"attempts",
		"claimed_at",
		"sent_at",
		"tracking_token",
		"first_opened_at",
		"last_opened_at",
		"open_count",
		"created_at",
		"updated_at",
	)
	requireHasUniqueIndex(t, recipient, "tracking_token")

	require.Equal(t, "mass_email_campaign", tableNameOf(t, campaign))
	require.Equal(t, "mass_email_recipient", tableNameOf(t, recipient))
}

func tableNameOf(t *testing.T, schema *load.Schema) string {
	t.Helper()
	if raw, ok := schema.Annotations["EntSQL"]; ok {
		if value, ok := raw.(map[string]any); ok {
			if table, ok := value["table"].(string); ok && table != "" {
				return table
			}
		}
	}
	return schema.Name
}
