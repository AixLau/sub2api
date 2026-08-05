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

// MerchantIntegration stores a merchant-facing SSO integration.
type MerchantIntegration struct {
	ent.Schema
}

func (MerchantIntegration) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "merchant_integrations"},
	}
}

func (MerchantIntegration) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (MerchantIntegration) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(120),
		field.String("code").NotEmpty().MaxLen(100),
		field.String("mode").Default("dynamic_api").MaxLen(32),
		field.String("merchant_code").Default("").MaxLen(120),
		field.String("description").Default("").MaxLen(500),
		field.String("status").Default("draft").MaxLen(20),
		field.Bool("enabled").Default(false),
		field.JSON("redirect_hosts", []string{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
	}
}

func (MerchantIntegration) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("endpoints", MerchantAPIEndpoint.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("bindings", MerchantBinding.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("recharge_records", MerchantRechargeRecord.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (MerchantIntegration) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code").Unique(),
		index.Fields("status", "enabled"),
	}
}
