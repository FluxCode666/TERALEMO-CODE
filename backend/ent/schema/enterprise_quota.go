package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

// EnterpriseQuota 定义企业数据实体；数据库结构以 SQL 迁移为权威。
type EnterpriseQuota struct{ ent.Schema }

func (EnterpriseQuota) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprise_quotas"}}
}

func (EnterpriseQuota) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (EnterpriseQuota) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("enterprise_id"),
		field.String("scope_type").MaxLen(20),
		field.Int64("scope_id"),
		field.Float("daily_limit_usd").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("weekly_limit_usd").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("monthly_limit_usd").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("total_limit_usd").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("daily_usage_usd").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("weekly_usage_usd").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("monthly_usage_usd").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Float("total_usage_usd").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}),
		field.Time("daily_window_start").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("weekly_window_start").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("monthly_window_start").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EnterpriseQuota) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope_type", "scope_id").Unique().StorageKey("idx_enterprise_quotas_scope_active").Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("enterprise_id").StorageKey("idx_enterprise_quotas_enterprise"),
	}
}
