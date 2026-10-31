package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AccountTestQuestion 定义管理员配置的账号测试题目。
type AccountTestQuestion struct {
	ent.Schema
}

func (AccountTestQuestion) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "account_test_questions"}}
}

func (AccountTestQuestion) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (AccountTestQuestion) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MaxRuneLen(100).NotEmpty().Unique(),
		field.Text("content").MaxRuneLen(20000).NotEmpty().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.JSON("answer_patterns", []string{}).
			Default([]string{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Bool("enabled").Default(true),
		field.Int("sort_order").Default(0),
	}
}

func (AccountTestQuestion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("sort_order", "id").StorageKey("account_test_questions_sort_order_id_idx"),
	}
}
