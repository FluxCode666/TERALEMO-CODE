package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"time"
)

// EnterpriseMember 定义企业数据实体；数据库结构以 SQL 迁移为权威。
type EnterpriseMember struct{ ent.Schema }

func (EnterpriseMember) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprise_members"}}
}

func (EnterpriseMember) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (EnterpriseMember) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("enterprise_id"),
		field.Int64("user_id"),
		field.Int64("department_id").Optional().Nillable(),
		field.String("role").MaxLen(20).Default("member"),
		field.String("status").MaxLen(20).Default("active"),
		field.String("title").MaxLen(100).Default(""),
		field.Time("joined_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EnterpriseMember) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("enterprise", Enterprise.Type).Unique().Required().Field("enterprise_id"),
		edge.To("user", User.Type).Unique().Required().Field("user_id"),
		edge.To("department", EnterpriseDepartment.Type).Unique().Field("department_id").Annotations(entsql.OnDelete(entsql.NoAction)),
	}
}

func (EnterpriseMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").Unique().StorageKey("idx_enterprise_members_user_active").Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("enterprise_id").StorageKey("idx_enterprise_members_enterprise"),
		index.Fields("department_id").StorageKey("idx_enterprise_members_department"),
	}
}
