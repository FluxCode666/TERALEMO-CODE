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

// MassEmailCampaign 邮件群发任务；数据库结构以 SQL 迁移 266_mass_email_campaigns.sql 为权威。
//
// 删除策略：硬删除（收件人通过外键级联删除）。
type MassEmailCampaign struct{ ent.Schema }

func (MassEmailCampaign) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "mass_email_campaign"}}
}

func (MassEmailCampaign) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (MassEmailCampaign) Fields() []ent.Field {
	return []ent.Field{
		field.String("subject").
			MaxLen(domain.MassEmailSubjectMaxLen).
			NotEmpty().
			Comment("邮件主题"),
		field.String("body_html").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default("").
			Comment("富文本 HTML 正文（已服务端净化）"),
		field.JSON("recipient_filter", domain.MassEmailRecipientFilter{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}).
			Comment("收件人条件 JSON"),
		field.String("status").
			MaxLen(20).
			Default(domain.MassEmailStatusDraft).
			Comment("状态: draft, scheduled, sending, completed, cancelled, failed"),
		field.Time("scheduled_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("定时发送时间（为空表示不定时）"),
		field.Int("total_count").
			Default(0).
			Comment("收件人总数"),
		field.Int("sent_count").
			Default(0).
			Comment("已发送数"),
		field.Int("failed_count").
			Default(0).
			Comment("失败数"),
		field.Int("opened_count").
			Default(0).
			Comment("已打开数"),
		field.Int64("created_by").
			Optional().
			Nillable().
			Comment("创建人用户ID（管理员）"),
		field.Time("started_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("开始发送时间"),
		field.Time("finished_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("结束时间（completed/failed/cancelled）"),
		field.String("last_error").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default("").
			Comment("最近一次整体失败原因"),
	}
}

func (MassEmailCampaign) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("recipients", MassEmailRecipient.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (MassEmailCampaign) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("created_at"),
		index.Fields("scheduled_at"),
		// 定时到期扫描：status='scheduled' AND scheduled_at <= now()
		index.Fields("status", "scheduled_at"),
	}
}
