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
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// Enterprise 定义企业数据实体；数据库结构以 SQL 迁移为权威。
type Enterprise struct{ ent.Schema }

func (Enterprise) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprises"}}
}

func (Enterprise) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}, mixins.SoftDeleteMixin{}}
}

func (Enterprise) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxLen(100).NotEmpty(),
		field.String("status").MaxLen(20).Default("active"),
		field.Bool("group_pool_configured").Default(false),
		field.Float("balance").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("total_recharged").Default(0).SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("low_balance_threshold").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.String("contact_name").MaxLen(100).Default(""),
		field.String("contact_email").MaxLen(255).Default(""),
		field.String("contact_phone").MaxLen(50).Default(""),
		field.String("notes").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.JSON("settings", domain.EnterpriseSettings{}).Default(domain.DefaultEnterpriseSettings).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (Enterprise) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("allowed_groups", Group.Type).Through("enterprise_allowed_groups", EnterpriseAllowedGroup.Type),
	}
}

func (Enterprise) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique().StorageKey("idx_enterprises_name_active").Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
