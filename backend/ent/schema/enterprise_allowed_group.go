package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"time"
)

// EnterpriseAllowedGroup 定义企业数据实体；数据库结构以 SQL 迁移为权威。
type EnterpriseAllowedGroup struct{ ent.Schema }

func (EnterpriseAllowedGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprise_allowed_groups"}, field.ID("enterprise_id", "group_id")}
}

func (EnterpriseAllowedGroup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("enterprise_id"),
		field.Int64("group_id"),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EnterpriseAllowedGroup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("enterprise", Enterprise.Type).Unique().Required().Field("enterprise_id"),
		edge.To("group", Group.Type).Unique().Required().Field("group_id").Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}
