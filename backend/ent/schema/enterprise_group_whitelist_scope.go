package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// EnterpriseGroupWhitelistScope records an explicitly configured layer even when
// group deletion has removed its last allowed group. SQL migrations are authoritative.
type EnterpriseGroupWhitelistScope struct{ ent.Schema }

func (EnterpriseGroupWhitelistScope) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprise_group_whitelist_scopes"}}
}

func (EnterpriseGroupWhitelistScope) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("enterprise_id"),
		field.String("scope_type").MaxLen(20),
		field.Int64("scope_id"),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EnterpriseGroupWhitelistScope) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope_type", "scope_id").Unique().StorageKey("uq_enterprise_group_whitelist_scopes_scope"),
		index.Fields("enterprise_id").StorageKey("idx_enterprise_group_whitelist_scopes_enterprise"),
	}
}
