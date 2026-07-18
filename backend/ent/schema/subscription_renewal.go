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

type SubscriptionRenewal struct {
	ent.Schema
}

func (SubscriptionRenewal) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "subscription_renewals"}}
}

func (SubscriptionRenewal) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (SubscriptionRenewal) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("subscription_id"),
		field.Int64("user_id"),
		field.Int64("target_group_id"),
		field.Int64("plan_id").Optional().Nillable(),
		field.String("source_type").MaxLen(20),
		field.String("source_id").MaxLen(64),
		field.Int("validity_days").Positive(),
		field.Float("monthly_limit_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).
			Default(0),
		field.String("status").MaxLen(20).Default("pending"),
		field.Time("activated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("notes").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
	}
}

func (SubscriptionRenewal) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_type", "source_id").Unique(),
		index.Fields("subscription_id", "status", "id"),
		index.Fields("user_id", "created_at"),
	}
}
