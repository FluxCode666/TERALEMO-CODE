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

// MassEmailRecipient 邮件群发的单个收件人；数据库结构以 SQL 迁移 266_mass_email_campaigns.sql 为权威。
//
// 唯一约束 (campaign_id, lower(email)) 是表达式唯一索引，ent schema 无法表达，以迁移为准。
// 项目启动时只执行 SQL 迁移，不调用 ent 的自动建表（Schema.Create），因此不会被 ent 覆盖。
type MassEmailRecipient struct{ ent.Schema }

func (MassEmailRecipient) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "mass_email_recipient"}}
}

func (MassEmailRecipient) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (MassEmailRecipient) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("campaign_id"),
		field.String("email").
			MaxLen(domain.MassEmailEmailMaxLen).
			NotEmpty().
			Comment("收件邮箱（小写）"),
		field.String("name").
			MaxLen(domain.MassEmailNameMaxLen).
			Default("").
			Comment("收件人显示名，用于 {{name}} 变量"),
		field.Int64("user_id").
			Optional().
			Nillable().
			Comment("关联用户ID（手动邮箱/企业联系人可为空）"),
		field.Int64("enterprise_id").
			Optional().
			Nillable().
			Comment("关联企业ID（用户/手动邮箱可为空）"),
		field.String("status").
			MaxLen(20).
			Default(domain.MassEmailRecipientStatusPending).
			Comment("状态: pending, sent, failed"),
		field.String("error").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default("").
			Comment("失败原因"),
		field.Int("attempts").
			Default(0).
			Comment("尝试次数"),
		field.Time("claimed_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("领取租约开始时间（超过租约可被重新领取）"),
		field.Time("sent_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("发送成功时间"),
		field.String("tracking_token").
			MaxLen(domain.MassEmailTrackingTokenLen).
			NotEmpty().
			Comment("打开追踪像素 token（唯一）"),
		field.Time("first_opened_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("首次打开时间"),
		field.Time("last_opened_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("最近打开时间"),
		field.Int("open_count").
			Default(0).
			Comment("打开次数"),
	}
}

func (MassEmailRecipient) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("campaign", MassEmailCampaign.Type).
			Ref("recipients").
			Field("campaign_id").
			Unique().
			Required(),
	}
}

func (MassEmailRecipient) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("campaign_id"),
		index.Fields("campaign_id", "status"),
		index.Fields("tracking_token").Unique().StorageKey("idx_mass_email_recipient_tracking_token"),
	}
}
