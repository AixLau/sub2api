package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
)

const contentModerationProviderTypeSafe = "typesafe"

// ContentModerationEngine is the small contract used by the main moderation
// pipeline. Engines return the same score shape, so keyword rules, caches,
// account scoping and enforcement stay independent from the upstream engine.
type ContentModerationEngine interface {
	Moderate(context.Context, *ContentModerationConfig, string, any, *int) (*moderationAPIResult, error)
}

type contentModerationEngineFunc func(context.Context, *ContentModerationConfig, string, any, *int) (*moderationAPIResult, error)

func (f contentModerationEngineFunc) Moderate(ctx context.Context, cfg *ContentModerationConfig, key string, input any, status *int) (*moderationAPIResult, error) {
	return f(ctx, cfg, key, input, status)
}

// typeSafeModerationQuestions is deliberately kept in the engine module. The
// main moderation pipeline only consumes category scores, so adding another
// engine does not require changing keyword matching, caching, or enforcement.
var typeSafeModerationQuestions = map[string]typesafe.Question{
	"harassment":             {Type: "noul", Instructions: "文本是否包含针对个人或群体的辱骂、贬损或骚扰？中立报道、批评行为本身、求助和反对骚扰不算。"},
	"harassment/threatening": {Type: "noul", Instructions: "文本是否在骚扰的同时表达可信的人身伤害或严重暴力威胁？中立报道、求助和防范威胁不算。"},
	"hate":                   {Type: "noul", Instructions: "文本是否因受保护身份而煽动仇恨、贬低或非人化某群体？中立报道和反仇恨引用不算。"},
	"hate/threatening":       {Type: "noul", Instructions: "文本是否因受保护身份而威胁、煽动或支持针对某群体的暴力？中立报道和反对该威胁不算。"},
	"illicit":                {Type: "noul", Instructions: "文本是否请求或提供实施违法行为的可操作帮助？合法防御、安全研究和举报不算。"},
	"illicit/violent":        {Type: "noul", Instructions: "文本是否请求或提供实施暴力违法行为的可操作帮助？合法安全防范和求助不算。"},
	"self-harm":              {Type: "noul", Instructions: "文本是否宣扬、鼓励或描写自杀、自残或进食障碍等自我伤害？预防教育、康复支持和劝阻不算。"},
	"self-harm/intent":       {Type: "noul", Instructions: "文本中的说话者是否表达正在进行或打算实施自我伤害？第三方报道、虚构或过去经历不等于当前意图。"},
	"self-harm/instructions": {Type: "noul", Instructions: "文本是否请求或提供自我伤害的具体方法、步骤或鼓励？危机援助和预防措施不算。"},
	"sexual":                 {Type: "noul", Instructions: "文本是否包含旨在引起性兴奋的露骨性描写、性行为或色情角色扮演？医学和性教育不算。"},
	"sexual/minors":          {Type: "noul", Instructions: "文本是否包含未成年人色情、性化描写或性行为？儿童保护、医疗教育和中立报道不算。"},
	"violence":               {Type: "noul", Instructions: "文本是否描写、鼓励或威胁造成身体伤害或死亡的暴力行为？防范建议和抽象讨论不算。"},
	"violence/graphic":       {Type: "noul", Instructions: "文本是否包含血腥、肢解、器官或重伤等露骨暴力细节？一般提及和医学教育不算。"},
}

func (s *ContentModerationService) callTypeSafeModeration(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input any, httpStatus *int) (*moderationAPIResult, error) {
	if cfg == nil {
		return nil, errors.New("typesafe moderation config is nil")
	}
	text := moderationTextInput(input)
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("typesafe moderation requires text input")
	}
	client, err := s.moderationHTTPClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	result, statusCode, err := typesafe.Evaluate(callCtx, client, cfg.BaseURL, apiKey, typesafe.Request{
		Model: cfg.Model, State: text, Questions: typeSafeModerationQuestions,
	})
	if httpStatus != nil {
		*httpStatus = statusCode
	}
	if err != nil {
		return nil, fmt.Errorf("typesafe moderation: %w", err)
	}
	flagged, _, _ := evaluateModerationScores(result.Scores, cfg.Thresholds)
	level := ModerationLevelPass
	if flagged {
		level = ModerationLevelReject
	}
	return moderationAPIResultFromProvider(ProviderModerationResult{
		Level: level, CategoryScores: result.Scores,
	}), nil
}

func moderationTextInput(input any) string {
	switch value := input.(type) {
	case string:
		return value
	case []moderationAPIInputPart:
		var parts []string
		for _, part := range value {
			if strings.TrimSpace(part.Text) != "" {
				parts = append(parts, part.Text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}
