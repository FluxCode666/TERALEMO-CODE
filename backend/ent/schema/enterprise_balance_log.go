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

// EnterpriseBalanceLog 定义企业数据实体；数据库结构以 SQL 迁移为权威。
type EnterpriseBalanceLog struct{ ent.Schema }

func (EnterpriseBalanceLog) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "enterprise_balance_logs"}}
}

func (EnterpriseBalanceLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("enterprise_id"),
		field.String("type").MaxLen(20),
		field.Float("amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("balance_before").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("balance_after").SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Int64("operator_user_id").Optional().Nillable(),
		field.String("notes").Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").Immutable().Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (EnterpriseBalanceLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("enterprise_id", "created_at").StorageKey("idx_enterprise_balance_logs_enterprise_created").Annotations(entsql.DescColumns("created_at")),
	}
}
