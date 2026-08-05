package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/merchantapiendpoint"
	"github.com/Wei-Shaw/sub2api/ent/merchantbinding"
	"github.com/Wei-Shaw/sub2api/ent/merchantintegration"
	"github.com/Wei-Shaw/sub2api/ent/merchantrechargerecord"
	"github.com/Wei-Shaw/sub2api/ent/user"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// MerchantSSOService implements dynamic_api merchant configuration and runtime calls.
type MerchantSSOService struct {
	client     *dbent.Client
	httpClient merchantHTTPDoer
}

func NewMerchantSSOService(client *dbent.Client) *MerchantSSOService {
	return &MerchantSSOService{
		client:     client,
		httpClient: newMerchantHTTPClient(),
	}
}

// SetHTTPClient is intentionally small and exists for deterministic service tests.
// Production wiring leaves the SSRF-safe client installed by the constructor.
func (s *MerchantSSOService) SetHTTPClient(client merchantHTTPDoer) {
	if client != nil {
		s.httpClient = client
	}
}

func (s *MerchantSSOService) ListIntegrations(ctx context.Context, includeDisabled bool) ([]MerchantIntegration, error) {
	if s.client == nil {
		return nil, infraerrors.New(500, "MERCHANT_SERVICE_UNAVAILABLE", "merchant integration service is unavailable")
	}
	query := s.client.MerchantIntegration.Query()
	if !includeDisabled {
		query = query.Where(
			merchantintegration.StatusEQ(MerchantStatusActive),
			merchantintegration.EnabledEQ(true),
		)
	}
	rows, err := query.Order(merchantintegration.ByName()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list merchant integrations: %w", err)
	}
	result := make([]MerchantIntegration, 0, len(rows))
	for _, row := range rows {
		endpoints, endpointErr := row.QueryEndpoints().Order(merchantapiendpoint.ByType()).All(ctx)
		if endpointErr != nil {
			return nil, fmt.Errorf("load merchant endpoints: %w", endpointErr)
		}
		result = append(result, merchantIntegrationView(row, endpoints))
	}
	return result, nil
}

func (s *MerchantSSOService) GetIntegration(ctx context.Context, id int64) (*MerchantIntegration, error) {
	row, endpoints, err := s.loadIntegration(ctx, id)
	if err != nil {
		return nil, err
	}
	result := merchantIntegrationView(row, endpoints)
	return &result, nil
}

func (s *MerchantSSOService) CreateIntegration(ctx context.Context, input MerchantIntegrationInput) (*MerchantIntegration, error) {
	input, err := normalizeMerchantIntegrationInput(input)
	if err != nil {
		return nil, err
	}
	if input.Enabled && input.Status == MerchantStatusActive {
		return nil, ErrMerchantNotReady
	}
	created, err := s.client.MerchantIntegration.Create().
		SetName(input.Name).
		SetCode(input.Code).
		SetMode(input.Mode).
		SetMerchantCode(input.MerchantCode).
		SetDescription(input.Description).
		SetStatus(input.Status).
		SetEnabled(input.Enabled).
		SetRedirectHosts(input.RedirectHosts).
		Save(ctx)
	if err != nil {
		if isEntConstraintError(err) {
			return nil, infraerrors.New(409, "MERCHANT_INTEGRATION_CODE_EXISTS", "merchant integration code already exists").WithCause(err)
		}
		return nil, fmt.Errorf("create merchant integration: %w", err)
	}
	result := merchantIntegrationView(created, nil)
	return &result, nil
}

func (s *MerchantSSOService) UpdateIntegration(ctx context.Context, id int64, input MerchantIntegrationInput) (*MerchantIntegration, error) {
	input, err := normalizeMerchantIntegrationInput(input)
	if err != nil {
		return nil, err
	}
	if _, err := s.client.MerchantIntegration.Get(ctx, id); err != nil {
		return nil, merchantNotFound(err, ErrMerchantIntegrationNotFound)
	}
	endpoints, err := s.client.MerchantAPIEndpoint.Query().
		Where(merchantapiendpoint.IntegrationIDEQ(id)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load merchant endpoints: %w", err)
	}
	if input.Enabled && input.Status == MerchantStatusActive {
		if err := validateMerchantIntegrationReady(input.RedirectHosts, endpoints); err != nil {
			return nil, err
		}
	}
	updated, err := s.client.MerchantIntegration.UpdateOneID(id).
		SetName(input.Name).
		SetCode(input.Code).
		SetMode(input.Mode).
		SetMerchantCode(input.MerchantCode).
		SetDescription(input.Description).
		SetStatus(input.Status).
		SetEnabled(input.Enabled).
		SetRedirectHosts(input.RedirectHosts).
		Save(ctx)
	if err != nil {
		if isEntConstraintError(err) {
			return nil, infraerrors.New(409, "MERCHANT_INTEGRATION_CODE_EXISTS", "merchant integration code already exists").WithCause(err)
		}
		return nil, fmt.Errorf("update merchant integration: %w", err)
	}
	return s.GetIntegration(ctx, updated.ID)
}

func (s *MerchantSSOService) SetIntegrationEnabled(ctx context.Context, id int64, enabled bool) (*MerchantIntegration, error) {
	row, endpoints, err := s.loadIntegration(ctx, id)
	if err != nil {
		return nil, err
	}
	if enabled {
		if err := validateMerchantIntegrationReady(row.RedirectHosts, endpoints); err != nil {
			return nil, err
		}
		row, err = s.client.MerchantIntegration.UpdateOneID(id).
			SetEnabled(true).
			SetStatus(MerchantStatusActive).
			Save(ctx)
	} else {
		row, err = s.client.MerchantIntegration.UpdateOneID(id).
			SetEnabled(false).
			SetStatus(MerchantStatusDisabled).
			Save(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("update merchant integration state: %w", err)
	}
	return s.GetIntegration(ctx, row.ID)
}

func (s *MerchantSSOService) CreateEndpoint(ctx context.Context, integrationID int64, input MerchantAPIEndpointInput) (*MerchantAPIEndpoint, error) {
	if _, err := s.client.MerchantIntegration.Get(ctx, integrationID); err != nil {
		return nil, merchantNotFound(err, ErrMerchantIntegrationNotFound)
	}
	input, err := normalizeMerchantEndpointInput(input)
	if err != nil {
		return nil, err
	}
	created, err := s.client.MerchantAPIEndpoint.Create().
		SetIntegrationID(integrationID).
		SetType(input.Type).
		SetURL(input.URL).
		SetMethod(input.Method).
		SetContentType(input.ContentType).
		SetQueryTemplate(input.QueryTemplate).
		SetHeaderTemplate(input.HeaderTemplate).
		SetBodyTemplate(input.BodyTemplate).
		SetAuthType(input.AuthType).
		SetSecretRef(input.SecretRef).
		SetResponseMapping(input.ResponseMapping).
		SetSuccessRule(input.SuccessRule).
		SetRetryPolicy(input.RetryPolicy).
		SetTimeoutMs(input.TimeoutMS).
		SetStatus(input.Status).
		SetEnabled(input.Enabled).
		Save(ctx)
	if err != nil {
		if isEntConstraintError(err) {
			return nil, infraerrors.New(409, "MERCHANT_ENDPOINT_TYPE_EXISTS", "endpoint type is already configured for this integration").WithCause(err)
		}
		return nil, fmt.Errorf("create merchant endpoint: %w", err)
	}
	result := merchantEndpointView(created)
	return &result, nil
}

func (s *MerchantSSOService) UpdateEndpoint(ctx context.Context, integrationID, id int64, input MerchantAPIEndpointInput) (*MerchantAPIEndpoint, error) {
	_, err := s.client.MerchantAPIEndpoint.Query().Where(
		merchantapiendpoint.IDEQ(id),
		merchantapiendpoint.IntegrationIDEQ(integrationID),
	).Only(ctx)
	if err != nil {
		return nil, merchantNotFound(err, ErrMerchantEndpointNotFound)
	}
	input, err = normalizeMerchantEndpointInput(input)
	if err != nil {
		return nil, err
	}
	updated, err := s.client.MerchantAPIEndpoint.UpdateOneID(id).
		SetType(input.Type).
		SetURL(input.URL).
		SetMethod(input.Method).
		SetContentType(input.ContentType).
		SetQueryTemplate(input.QueryTemplate).
		SetHeaderTemplate(input.HeaderTemplate).
		SetBodyTemplate(input.BodyTemplate).
		SetAuthType(input.AuthType).
		SetSecretRef(input.SecretRef).
		SetResponseMapping(input.ResponseMapping).
		SetSuccessRule(input.SuccessRule).
		SetRetryPolicy(input.RetryPolicy).
		SetTimeoutMs(input.TimeoutMS).
		SetStatus(input.Status).
		SetEnabled(input.Enabled).
		Save(ctx)
	if err != nil {
		if isEntConstraintError(err) {
			return nil, infraerrors.New(409, "MERCHANT_ENDPOINT_TYPE_EXISTS", "endpoint type is already configured for this integration").WithCause(err)
		}
		return nil, fmt.Errorf("update merchant endpoint: %w", err)
	}
	result := merchantEndpointView(updated)
	return &result, nil
}

func (s *MerchantSSOService) SetEndpointEnabled(ctx context.Context, integrationID, id int64, enabled bool) (*MerchantAPIEndpoint, error) {
	_, err := s.client.MerchantAPIEndpoint.Query().Where(
		merchantapiendpoint.IDEQ(id),
		merchantapiendpoint.IntegrationIDEQ(integrationID),
	).Only(ctx)
	if err != nil {
		return nil, merchantNotFound(err, ErrMerchantEndpointNotFound)
	}
	status := MerchantEndpointStatusDisabled
	if enabled {
		status = MerchantEndpointStatusActive
	}
	updated, err := s.client.MerchantAPIEndpoint.UpdateOneID(id).
		SetEnabled(enabled).
		SetStatus(status).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update merchant endpoint state: %w", err)
	}
	result := merchantEndpointView(updated)
	return &result, nil
}

func (s *MerchantSSOService) ValidateIntegrationReady(ctx context.Context, id int64) error {
	row, endpoints, err := s.loadIntegration(ctx, id)
	if err != nil {
		return err
	}
	return validateMerchantIntegrationReady(row.RedirectHosts, endpoints)
}

func (s *MerchantSSOService) ListPublicIntegrations(ctx context.Context) ([]MerchantPublicIntegration, error) {
	rows, err := s.client.MerchantIntegration.Query().
		Where(merchantintegration.StatusEQ(MerchantStatusActive), merchantintegration.EnabledEQ(true)).
		Order(merchantintegration.ByName()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list public merchant integrations: %w", err)
	}
	result := make([]MerchantPublicIntegration, 0, len(rows))
	for _, row := range rows {
		if err := s.ValidateIntegrationReady(ctx, row.ID); err != nil {
			continue
		}
		result = append(result, MerchantPublicIntegration{
			ID:          row.ID,
			Name:        row.Name,
			Code:        row.Code,
			Description: row.Description,
		})
	}
	return result, nil
}

func (s *MerchantSSOService) Launch(ctx context.Context, integrationID, userID int64) (*MerchantLaunchResult, error) {
	integration, endpoints, err := s.loadIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	if !integration.Enabled || integration.Status != MerchantStatusActive {
		return nil, ErrMerchantNotReady
	}
	if err := validateMerchantIntegrationReady(integration.RedirectHosts, endpoints); err != nil {
		return nil, err
	}
	platformUser, err := s.client.User.Query().Where(user.IDEQ(userID)).
		WithAttributeValues(func(query *dbent.UserAttributeValueQuery) {
			query.WithDefinition()
		}).Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, infraerrors.New(404, "USER_NOT_FOUND", "user not found").WithCause(err)
		}
		return nil, fmt.Errorf("load merchant user: %w", err)
	}
	binding, err := s.findActiveBinding(ctx, integrationID, userID)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		registration := activeMerchantEndpoint(endpoints, MerchantEndpointRegisterLogin)
		if registration == nil {
			registration = activeMerchantEndpoint(endpoints, MerchantEndpointRegister)
		}
		if registration == nil {
			return nil, ErrMerchantNotReady
		}
		call, callErr := s.callMerchantEndpoint(ctx, integration, registration, platformUser, nil, MerchantRechargeQuery{})
		if callErr != nil {
			return nil, callErr
		}
		if !call.Response.Successful {
			return nil, merchantBusinessError(call.Response)
		}
		if strings.TrimSpace(call.Response.ExternalUserID) == "" {
			return nil, ErrMerchantResponseInvalid.WithCause(errors.New("registration response did not contain external user id"))
		}
		binding, err = s.upsertBinding(ctx, integrationID, userID, call.Response.ExternalUserID, call.Response.ExternalAccount, nil)
		if err != nil {
			return nil, err
		}
		if call.Response.RedirectURL != "" {
			redirect, err := validateMerchantRedirectURL(call.Response.RedirectURL, integration.RedirectHosts)
			if err != nil {
				return nil, err
			}
			binding, err = s.touchBindingLogin(ctx, binding, call.Response.ExternalUserID, call.Response.ExternalAccount)
			if err != nil {
				return nil, err
			}
			return &MerchantLaunchResult{
				IntegrationID:   integrationID,
				BindingID:       binding.ID,
				ExternalUserID:  binding.ExternalUserID,
				ExternalAccount: binding.ExternalAccount,
				RedirectURL:     redirect,
			}, nil
		}
	}

	loginEndpoint := activeMerchantEndpoint(endpoints, MerchantEndpointLogin)
	if loginEndpoint == nil {
		loginEndpoint = activeMerchantEndpoint(endpoints, MerchantEndpointToken)
	}
	if loginEndpoint == nil {
		return nil, ErrMerchantNotReady
	}
	call, callErr := s.callMerchantEndpoint(ctx, integration, loginEndpoint, platformUser, binding, MerchantRechargeQuery{})
	if callErr != nil {
		return nil, callErr
	}
	if !call.Response.Successful {
		return nil, merchantBusinessError(call.Response)
	}
	if call.Response.RedirectURL == "" && loginEndpoint.Type == MerchantEndpointLogin {
		if tokenEndpoint := activeMerchantEndpoint(endpoints, MerchantEndpointToken); tokenEndpoint != nil {
			call, callErr = s.callMerchantEndpoint(ctx, integration, tokenEndpoint, platformUser, binding, MerchantRechargeQuery{})
			if callErr != nil {
				return nil, callErr
			}
			if !call.Response.Successful {
				return nil, merchantBusinessError(call.Response)
			}
		}
	}
	if call.Response.RedirectURL == "" {
		return nil, ErrMerchantResponseInvalid.WithCause(errors.New("login response did not contain redirect_url"))
	}
	redirect, err := validateMerchantRedirectURL(call.Response.RedirectURL, integration.RedirectHosts)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, ErrMerchantResponseInvalid.WithCause(errors.New("login response has no binding"))
	}
	if call.Response.ExternalUserID != "" || call.Response.ExternalAccount != "" {
		binding, err = s.touchBindingLogin(ctx, binding, nonEmptyOr(binding.ExternalUserID, call.Response.ExternalUserID), nonEmptyOr(binding.ExternalAccount, call.Response.ExternalAccount))
	} else {
		binding, err = s.touchBindingLogin(ctx, binding, binding.ExternalUserID, binding.ExternalAccount)
	}
	if err != nil {
		return nil, err
	}
	return &MerchantLaunchResult{
		IntegrationID:   integrationID,
		BindingID:       binding.ID,
		ExternalUserID:  binding.ExternalUserID,
		ExternalAccount: binding.ExternalAccount,
		RedirectURL:     redirect,
	}, nil
}

func (s *MerchantSSOService) TestEndpoint(ctx context.Context, integrationID, endpointID, userID int64, query MerchantRechargeQuery) (*MerchantTestResult, error) {
	integration, _, err := s.loadIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	endpoint, err := s.client.MerchantAPIEndpoint.Query().
		Where(merchantapiendpoint.IDEQ(endpointID), merchantapiendpoint.IntegrationIDEQ(integrationID)).
		Only(ctx)
	if err != nil {
		return nil, merchantNotFound(err, ErrMerchantEndpointNotFound)
	}
	var platformUser *dbent.User
	if userID > 0 {
		platformUser, err = s.client.User.Query().Where(user.IDEQ(userID)).
			WithAttributeValues(func(query *dbent.UserAttributeValueQuery) {
				query.WithDefinition()
			}).Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("load merchant test user: %w", err)
		}
	}
	call, err := s.callMerchantEndpoint(ctx, integration, endpoint, platformUser, nil, query)
	if err != nil {
		return nil, err
	}
	return &MerchantTestResult{
		EndpointID:  endpointID,
		HTTPStatus:  call.HTTPStatus,
		Successful:  call.Response.Successful,
		Code:        call.Response.Code,
		Message:     call.Response.Message,
		RedirectURL: call.Response.RedirectURL,
		Response:    call.Response.Payload,
	}, nil
}

func (s *MerchantSSOService) ListBindingsByUser(ctx context.Context, userID int64) ([]MerchantBinding, error) {
	rows, err := s.client.MerchantBinding.Query().
		Where(merchantbinding.UserIDEQ(userID)).
		WithIntegration().
		Order(merchantbinding.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list merchant bindings: %w", err)
	}
	result := make([]MerchantBinding, 0, len(rows))
	for _, row := range rows {
		view := merchantBindingView(row)
		if row.Edges.Integration != nil {
			available, endpointErr := row.Edges.Integration.QueryEndpoints().Where(
				merchantapiendpoint.TypeEQ(MerchantEndpointRecharge),
				merchantapiendpoint.StatusEQ(MerchantEndpointStatusActive),
				merchantapiendpoint.EnabledEQ(true),
			).Exist(ctx)
			if endpointErr != nil {
				return nil, fmt.Errorf("check merchant recharge endpoint: %w", endpointErr)
			}
			view.RechargeSyncAvailable = available
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *MerchantSSOService) ListRechargeRecords(ctx context.Context, userID, bindingID int64, page, pageSize int) ([]MerchantRechargeRecord, int64, error) {
	binding, err := s.getBindingForUser(ctx, bindingID, userID)
	if err != nil {
		return nil, 0, err
	}
	query := s.client.MerchantRechargeRecord.Query().Where(
		merchantrechargerecord.IntegrationIDEQ(binding.IntegrationID),
		merchantrechargerecord.UserIDEQ(userID),
	)
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count merchant recharge records: %w", err)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20
	}
	rows, err := query.Order(merchantrechargerecord.ByMerchantCreatedAt()).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list merchant recharge records: %w", err)
	}
	result := make([]MerchantRechargeRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, merchantRechargeRecordView(row))
	}
	return result, int64(total), nil
}

func (s *MerchantSSOService) SyncRechargeRecords(ctx context.Context, userID, bindingID int64, query MerchantRechargeQuery) (*MerchantRechargeSyncResult, error) {
	binding, err := s.getBindingForUser(ctx, bindingID, userID)
	if err != nil {
		return nil, err
	}
	integration, endpoints, err := s.loadIntegration(ctx, binding.IntegrationID)
	if err != nil {
		return nil, err
	}
	rechargeEndpoint := activeMerchantEndpoint(endpoints, MerchantEndpointRecharge)
	if rechargeEndpoint == nil {
		return nil, ErrMerchantEndpointNotFound.WithCause(errors.New("recharge_records endpoint is not configured"))
	}
	platformUser, err := s.client.User.Query().Where(user.IDEQ(userID)).
		WithAttributeValues(func(query *dbent.UserAttributeValueQuery) {
			query.WithDefinition()
		}).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load merchant recharge user: %w", err)
	}
	call, err := s.callMerchantEndpoint(ctx, integration, rechargeEndpoint, platformUser, binding, query)
	if err != nil {
		return nil, err
	}
	if !call.Response.Successful {
		return nil, merchantBusinessError(call.Response)
	}
	mapping := endpointResponseMapping(rechargeEndpoint)
	recordsPath := mappingPath(mapping, "recordsPath", "records_path", "data.records")
	rawRecords, ok := getMerchantPath(call.Response.Payload, recordsPath)
	if !ok {
		return nil, ErrMerchantResponseInvalid.WithCause(errors.New("recharge response did not contain records"))
	}
	records, ok := rawRecords.([]any)
	if !ok {
		return nil, ErrMerchantResponseInvalid.WithCause(errors.New("recharge records must be an array"))
	}
	result := &MerchantRechargeSyncResult{BindingID: binding.ID, Records: make([]MerchantRechargeRecord, 0, len(records))}
	for _, raw := range records {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, ErrMerchantResponseInvalid.WithCause(errors.New("recharge record must be an object"))
		}
		row, err := s.upsertRechargeRecord(ctx, integration.ID, userID, binding.ExternalUserID, item, mapping)
		if err != nil {
			return nil, err
		}
		result.Records = append(result.Records, merchantRechargeRecordView(row))
		result.Synced++
	}
	now := time.Now()
	if _, err := s.client.MerchantBinding.UpdateOneID(binding.ID).SetLastRechargeSyncAt(now).SetLastSyncAt(now).Save(ctx); err != nil {
		return nil, fmt.Errorf("update merchant recharge sync time: %w", err)
	}
	return result, nil
}

func (s *MerchantSSOService) loadIntegration(ctx context.Context, id int64) (*dbent.MerchantIntegration, []*dbent.MerchantAPIEndpoint, error) {
	row, err := s.client.MerchantIntegration.Get(ctx, id)
	if err != nil {
		return nil, nil, merchantNotFound(err, ErrMerchantIntegrationNotFound)
	}
	endpoints, err := row.QueryEndpoints().Order(merchantapiendpoint.ByType()).All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load merchant integration endpoints: %w", err)
	}
	return row, endpoints, nil
}

func (s *MerchantSSOService) findActiveBinding(ctx context.Context, integrationID, userID int64) (*dbent.MerchantBinding, error) {
	row, err := s.client.MerchantBinding.Query().Where(
		merchantbinding.IntegrationIDEQ(integrationID),
		merchantbinding.UserIDEQ(userID),
		merchantbinding.StatusEQ("active"),
	).Only(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load merchant binding: %w", err)
	}
	return row, nil
}

func (s *MerchantSSOService) getBindingForUser(ctx context.Context, bindingID, userID int64) (*dbent.MerchantBinding, error) {
	row, err := s.client.MerchantBinding.Query().Where(
		merchantbinding.IDEQ(bindingID),
		merchantbinding.UserIDEQ(userID),
	).Only(ctx)
	if err != nil {
		return nil, merchantNotFound(err, ErrMerchantBindingNotFound)
	}
	return row, nil
}

func (s *MerchantSSOService) upsertBinding(ctx context.Context, integrationID, userID int64, externalUserID, externalAccount string, lastLoginAt *time.Time) (*dbent.MerchantBinding, error) {
	externalUserID = strings.TrimSpace(externalUserID)
	if externalUserID == "" {
		return nil, ErrMerchantResponseInvalid.WithCause(errors.New("external user id is empty"))
	}
	builder := s.client.MerchantBinding.Create().
		SetIntegrationID(integrationID).
		SetUserID(userID).
		SetExternalUserID(externalUserID).
		SetExternalAccount(strings.TrimSpace(externalAccount)).
		SetStatus("active")
	if lastLoginAt != nil {
		builder.SetLastLoginAt(*lastLoginAt)
	}
	rowID, err := builder.OnConflictColumns(merchantbinding.FieldIntegrationID, merchantbinding.FieldUserID).
		UpdateNewValues().
		ID(ctx)
	if err != nil {
		return nil, fmt.Errorf("save merchant binding: %w", err)
	}
	row, err := s.client.MerchantBinding.Get(ctx, rowID)
	if err != nil {
		return nil, fmt.Errorf("reload merchant binding: %w", err)
	}
	return row, nil
}

func (s *MerchantSSOService) touchBindingLogin(ctx context.Context, binding *dbent.MerchantBinding, externalUserID, externalAccount string) (*dbent.MerchantBinding, error) {
	now := time.Now()
	updater := s.client.MerchantBinding.UpdateOneID(binding.ID).
		SetStatus("active").
		SetLastLoginAt(now).
		SetExternalUserID(strings.TrimSpace(externalUserID))
	if strings.TrimSpace(externalAccount) != "" {
		updater = updater.SetExternalAccount(strings.TrimSpace(externalAccount))
	}
	row, err := updater.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("update merchant binding login time: %w", err)
	}
	return row, nil
}

func (s *MerchantSSOService) upsertRechargeRecord(ctx context.Context, integrationID, userID int64, externalUserID string, item map[string]any, mapping map[string]any) (*dbent.MerchantRechargeRecord, error) {
	orderNo := mappedRecordValue(item, mapping, "orderNo", "order_no")
	if orderNo == "" {
		return nil, ErrMerchantResponseInvalid.WithCause(errors.New("recharge record order_no is required"))
	}
	createdAtValue, ok := mappedRecordRawValue(item, mapping, "createdAt", "created_at")
	if !ok {
		return nil, ErrMerchantResponseInvalid.WithCause(errors.New("recharge record created_at is required"))
	}
	createdAt, err := parseMerchantTime(createdAtValue)
	if err != nil {
		return nil, ErrMerchantResponseInvalid.WithCause(err)
	}
	externalRecordUserID := mappedRecordValue(item, mapping, "userId", "user_id")
	if externalRecordUserID == "" {
		externalRecordUserID = externalUserID
	}
	builder := s.client.MerchantRechargeRecord.Create().
		SetIntegrationID(integrationID).
		SetUserID(userID).
		SetExternalUserID(externalRecordUserID).
		SetOrderNo(orderNo).
		SetAmount(mappedRecordValue(item, mapping, "amount")).
		SetCurrency(mappedRecordValue(item, mapping, "currency")).
		SetBalanceBefore(mappedRecordValue(item, mapping, "balanceBefore", "balance_before")).
		SetBalanceAfter(mappedRecordValue(item, mapping, "balanceAfter", "balance_after")).
		SetChargeType(mappedRecordValue(item, mapping, "chargeType", "charge_type")).
		SetPayMethod(mappedRecordValue(item, mapping, "payMethod", "pay_method")).
		SetStatus(mappedRecordValue(item, mapping, "status")).
		SetPlatformOrderNo(mappedRecordValue(item, mapping, "platformOrderNo", "platform_order_no")).
		SetMerchantCreatedAt(createdAt)
	rowID, err := builder.OnConflictColumns(
		merchantrechargerecord.FieldIntegrationID,
		merchantrechargerecord.FieldUserID,
		merchantrechargerecord.FieldOrderNo,
		merchantrechargerecord.FieldMerchantCreatedAt,
	).UpdateNewValues().ID(ctx)
	if err != nil {
		return nil, fmt.Errorf("save merchant recharge record: %w", err)
	}
	row, err := s.client.MerchantRechargeRecord.Get(ctx, rowID)
	if err != nil {
		return nil, fmt.Errorf("reload merchant recharge record: %w", err)
	}
	return row, nil
}

func validateMerchantIntegrationReady(hosts []string, endpoints []*dbent.MerchantAPIEndpoint) error {
	if len(hosts) == 0 {
		return ErrMerchantNotReady.WithCause(errors.New("redirect host allowlist is empty"))
	}
	register := activeMerchantEndpoint(endpoints, MerchantEndpointRegisterLogin)
	if register == nil {
		register = activeMerchantEndpoint(endpoints, MerchantEndpointRegister)
	}
	if register == nil {
		return ErrMerchantNotReady.WithCause(errors.New("registration endpoint is missing"))
	}
	if activeMerchantEndpoint(endpoints, MerchantEndpointLogin) == nil && activeMerchantEndpoint(endpoints, MerchantEndpointToken) == nil {
		return ErrMerchantNotReady.WithCause(errors.New("login or token endpoint is missing"))
	}
	if activeMerchantEndpoint(endpoints, MerchantEndpointRecharge) == nil {
		return ErrMerchantNotReady.WithCause(errors.New("recharge_records endpoint is missing"))
	}
	return nil
}

func activeMerchantEndpoint(endpoints []*dbent.MerchantAPIEndpoint, endpointType string) *dbent.MerchantAPIEndpoint {
	for _, endpoint := range endpoints {
		if endpoint.Type == endpointType && endpoint.Enabled && endpoint.Status == MerchantEndpointStatusActive {
			return endpoint
		}
	}
	return nil
}

func validateMerchantRedirectURL(raw string, hosts []string) (string, error) {
	validated, err := urlvalidatorForMerchantRedirect(raw, hosts)
	if err != nil {
		return "", ErrMerchantResponseInvalid.WithCause(err)
	}
	return validated, nil
}

func urlvalidatorForMerchantRedirect(raw string, hosts []string) (string, error) {
	return validateMerchantRedirectWithAllowlist(raw, hosts)
}

func merchantBusinessError(response merchantResponse) error {
	message := strings.TrimSpace(response.Message)
	if message == "" {
		message = "merchant rejected the request"
	}
	metadata := map[string]string{}
	if response.Code != "" {
		metadata["merchant_code"] = response.Code
	}
	return infraerrors.New(502, "MERCHANT_API_BUSINESS_ERROR", message).WithMetadata(metadata)
}

func endpointResponseMapping(endpoint *dbent.MerchantAPIEndpoint) map[string]any {
	return normalizeMerchantResponseMapping(endpoint.ResponseMapping)
}

func mappingPath(mapping map[string]any, first, second, fallback string) string {
	for _, key := range []string{first, second} {
		if value, ok := mapping[key]; ok {
			if text, ok := merchantStringValue(value); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return fallback
}

func mappedRecordRawValue(item map[string]any, mapping map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		path := mappingPath(mapping, key, snakeCaseMerchantKey(key), key)
		if value, ok := getMerchantPath(item, path); ok {
			return value, true
		}
	}
	return nil, false
}

func mappedRecordValue(item map[string]any, mapping map[string]any, keys ...string) string {
	value, _ := mappedRecordRawValue(item, mapping, keys...)
	result, _ := merchantStringValue(value)
	return strings.TrimSpace(result)
}

func snakeCaseMerchantKey(value string) string {
	var result strings.Builder
	for i, r := range value {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteByte(byte(r + ('a' - 'A')))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func nonEmptyOr(original, replacement string) string {
	if strings.TrimSpace(replacement) != "" {
		return strings.TrimSpace(replacement)
	}
	return original
}

func merchantIntegrationView(row *dbent.MerchantIntegration, endpoints []*dbent.MerchantAPIEndpoint) MerchantIntegration {
	result := MerchantIntegration{
		ID:            row.ID,
		Name:          row.Name,
		Code:          row.Code,
		Mode:          row.Mode,
		MerchantCode:  row.MerchantCode,
		Description:   row.Description,
		Status:        row.Status,
		Enabled:       row.Enabled,
		RedirectHosts: append([]string(nil), row.RedirectHosts...),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
	if endpoints != nil {
		result.Endpoints = make([]MerchantAPIEndpoint, 0, len(endpoints))
		for _, endpoint := range endpoints {
			result.Endpoints = append(result.Endpoints, merchantEndpointView(endpoint))
		}
	}
	return result
}

func merchantEndpointView(row *dbent.MerchantAPIEndpoint) MerchantAPIEndpoint {
	return MerchantAPIEndpoint{
		ID:              row.ID,
		IntegrationID:   row.IntegrationID,
		Type:            row.Type,
		URL:             row.URL,
		Method:          row.Method,
		ContentType:     row.ContentType,
		QueryTemplate:   cloneMerchantMap(row.QueryTemplate),
		HeaderTemplate:  cloneMerchantMap(row.HeaderTemplate),
		BodyTemplate:    cloneMerchantMap(row.BodyTemplate),
		AuthType:        row.AuthType,
		SecretRef:       row.SecretRef,
		ResponseMapping: cloneMerchantMap(row.ResponseMapping),
		SuccessRule:     cloneMerchantMap(row.SuccessRule),
		RetryPolicy:     cloneMerchantMap(row.RetryPolicy),
		TimeoutMS:       row.TimeoutMs,
		Status:          row.Status,
		Enabled:         row.Enabled,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func merchantBindingView(row *dbent.MerchantBinding) MerchantBinding {
	result := MerchantBinding{
		ID:                 row.ID,
		IntegrationID:      row.IntegrationID,
		UserID:             row.UserID,
		ExternalUserID:     row.ExternalUserID,
		ExternalAccount:    row.ExternalAccount,
		Status:             row.Status,
		LastLoginAt:        row.LastLoginAt,
		LastSyncAt:         row.LastSyncAt,
		LastRechargeSyncAt: row.LastRechargeSyncAt,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
	if row.Edges.Integration != nil {
		result.IntegrationName = row.Edges.Integration.Name
		result.IntegrationCode = row.Edges.Integration.Code
	}
	return result
}

func merchantRechargeRecordView(row *dbent.MerchantRechargeRecord) MerchantRechargeRecord {
	return MerchantRechargeRecord{
		ID:                row.ID,
		IntegrationID:     row.IntegrationID,
		UserID:            row.ExternalUserID,
		PlatformUserID:    row.UserID,
		OrderNo:           row.OrderNo,
		Amount:            row.Amount,
		Currency:          row.Currency,
		BalanceBefore:     row.BalanceBefore,
		BalanceAfter:      row.BalanceAfter,
		ChargeType:        row.ChargeType,
		PayMethod:         row.PayMethod,
		Status:            row.Status,
		PlatformOrderNo:   row.PlatformOrderNo,
		MerchantCreatedAt: row.MerchantCreatedAt,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func isEntConstraintError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique constraint") || strings.Contains(text, "duplicate key") || strings.Contains(text, "unique violation")
}
