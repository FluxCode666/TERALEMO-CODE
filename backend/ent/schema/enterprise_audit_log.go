package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

// EnterpriseAuditLog 定义企业数据实体；数据库结构以 SQL 迁移为权威。
type EnterpriseAuditLog struct{ ent.Schema }

func (EnterpriseAuditLog) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprise_audit_logs"}}
}

func (EnterpriseAuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("enterprise_id"),
		field.Int64("actor_user_id"),
		field.String("actor_role").MaxLen(20),
		field.String("action").MaxLen(64),
		field.String("target_type").MaxLen(32).Default(""),
		field.Int64("target_id").Optional().Nillable(),
		field.JSON("detail", map[string]any{}).Default(func() map[string]any { return map[string]any{} }).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EnterpriseAuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enterprise_id", "created_at").StorageKey("idx_enterprise_audit_logs_enterprise_created").Annotations(entsql.DescColumns("created_at")),
	}
}
