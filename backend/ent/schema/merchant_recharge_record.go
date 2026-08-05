package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// MerchantRechargeRecord is an idempotently synchronized merchant recharge row.
type MerchantRechargeRecord struct {
	ent.Schema
}

func (MerchantRechargeRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "merchant_recharge_records"},
	}
}

func (MerchantRechargeRecord) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (MerchantRechargeRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("integration_id"),
		field.Int64("user_id"),
		field.String("external_user_id").Default("").MaxLen(255),
		field.String("order_no").NotEmpty().MaxLen(128),
		field.String("amount").Default("").MaxLen(64),
		field.String("currency").Default("").MaxLen(16),
		field.String("balance_before").Default("").MaxLen(64),
		field.String("balance_after").Default("").MaxLen(64),
		field.String("charge_type").Default("").MaxLen(32),
		field.String("pay_method").Default("").MaxLen(32),
		field.String("status").Default("").MaxLen(32),
		field.String("platform_order_no").Default("").MaxLen(128),
		field.Time("merchant_created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (MerchantRechargeRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("integration", MerchantIntegration.Type).
			Ref("recharge_records").
			Field("integration_id").
			Unique().
			Required(),
		edge.From("user", User.Type).
			Ref("merchant_recharge_records").
			Field("user_id").
			Unique().
			Required(),
	}
}

func (MerchantRechargeRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("integration_id", "user_id", "order_no", "merchant_created_at").Unique(),
		index.Fields("user_id", "integration_id", "merchant_created_at"),
	}
}
