package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

// EnterpriseGroupWhitelist 定义企业数据实体；数据库结构以 SQL 迁移为权威。
type EnterpriseGroupWhitelist struct{ ent.Schema }

func (EnterpriseGroupWhitelist) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprise_group_whitelists"}}
}

func (EnterpriseGroupWhitelist) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("enterprise_id"),
		field.String("scope_type").MaxLen(20),
		field.Int64("scope_id"),
		field.Int64("group_id"),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EnterpriseGroupWhitelist) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("group", Group.Type).Unique().Required().Field("group_id").Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (EnterpriseGroupWhitelist) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope_type", "scope_id", "group_id").Unique().StorageKey("idx_enterprise_group_whitelists_scope_group"),
		index.Fields("enterprise_id").StorageKey("idx_enterprise_group_whitelists_enterprise"),
	}
}
