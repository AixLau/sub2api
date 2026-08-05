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

// MerchantBinding links a platform user to an account at one merchant.
type MerchantBinding struct {
	ent.Schema
}

func (MerchantBinding) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "merchant_bindings"},
	}
}

func (MerchantBinding) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (MerchantBinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("integration_id"),
		field.Int64("user_id"),
		field.String("external_user_id").NotEmpty().MaxLen(255),
		field.String("external_account").Default("").MaxLen(255),
		field.String("status").Default("active").MaxLen(20),
		field.Time("last_login_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_sync_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_recharge_sync_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (MerchantBinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("integration", MerchantIntegration.Type).
			Ref("bindings").
			Field("integration_id").
			Unique().
			Required(),
		edge.From("user", User.Type).
			Ref("merchant_bindings").
			Field("user_id").
			Unique().
			Required(),
	}
}

func (MerchantBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("integration_id", "user_id").Unique(),
		index.Fields("integration_id", "external_user_id"),
		index.Fields("user_id", "status"),
	}
}
