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

// MerchantAPIEndpoint describes one dynamic_api request and its response rules.
type MerchantAPIEndpoint struct {
	ent.Schema
}

func (MerchantAPIEndpoint) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "merchant_api_endpoints"},
	}
}

func (MerchantAPIEndpoint) Mixin() []ent.Mixin {
	return []ent.Mixin{mixins.TimeMixin{}}
}

func (MerchantAPIEndpoint) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("integration_id"),
		field.String("type").NotEmpty().MaxLen(32),
		field.String("url").NotEmpty().MaxLen(2048),
		field.String("method").Default("POST").MaxLen(10),
		field.String("content_type").Default("application/json").MaxLen(80),
		field.JSON("query_template", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("header_template", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("body_template", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("auth_type").Default("none").MaxLen(20),
		field.String("secret_ref").Default("").MaxLen(255),
		field.JSON("response_mapping", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("success_rule", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.JSON("retry_policy", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Int("timeout_ms").Default(10000),
		field.String("status").Default("active").MaxLen(20),
		field.Bool("enabled").Default(true),
	}
}

func (MerchantAPIEndpoint) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("integration", MerchantIntegration.Type).
			Ref("endpoints").
			Field("integration_id").
			Unique().
			Required(),
	}
}

func (MerchantAPIEndpoint) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("integration_id", "type").Unique(),
		index.Fields("integration_id", "enabled", "status"),
	}
}
