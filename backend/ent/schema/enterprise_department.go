package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

// EnterpriseDepartment 定义企业数据实体；数据库结构以 SQL 迁移为权威。
type EnterpriseDepartment struct{ ent.Schema }

func (EnterpriseDepartment) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprise_departments"}}
}

func (EnterpriseDepartment) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (EnterpriseDepartment) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("enterprise_id"),
		field.String("name").MaxLen(100).NotEmpty(),
		field.Int("sort_order").Default(0),
	}
}

func (EnterpriseDepartment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("enterprise", Enterprise.Type).Unique().Required().Field("enterprise_id"),
	}
}

func (EnterpriseDepartment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enterprise_id", "name").Unique().StorageKey("idx_enterprise_departments_name_active").Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
