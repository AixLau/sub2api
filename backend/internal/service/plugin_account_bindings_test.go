package service

import (
	"context"
	"errors"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type accountBindingsRepository struct {
	PluginRepository
	installation  *PluginInstallation
	validationErr error
	saveErr       error
	saveCalls     int
	onSave        func()
}

func (r *accountBindingsRepository) GetByID(context.Context, int64) (*PluginInstallation, error) {
	copy := *r.installation
	copy.Bindings = clonePluginBindings(r.installation.Bindings)
	return &copy, nil
}

func (r *accountBindingsRepository) List(ctx context.Context) ([]*PluginInstallation, error) {
	installation, err := r.GetByID(ctx, r.installation.ID)
	return []*PluginInstallation{installation}, err
}

func (r *accountBindingsRepository) ValidateOpenAIOAuthAccounts(context.Context, []int64) error {
	return r.validationErr
}

func (r *accountBindingsRepository) UpdateBindingsAndState(_ context.Context, id int64, bindings []PluginBinding, state, lastError string, enabledAt *time.Time, expectedState, expectedSHA string) error {
	r.saveCalls++
	if r.saveErr != nil {
		return r.saveErr
	}
	if id != r.installation.ID || expectedState != r.installation.State || expectedSHA != r.installation.BinarySHA256 {
		return ErrPluginStateChanged
	}
	r.installation.Bindings = clonePluginBindings(bindings)
	r.installation.State = state
	r.installation.LastError = lastError
	r.installation.EnabledAt = enabledAt
	if r.onSave != nil {
		r.onSave()
	}
	return nil
}

type accountBindingsHealthClient struct {
	pluginv1.TransportPluginClient
	healthCalls int
}

func (c *accountBindingsHealthClient) Health(context.Context, *pluginv1.HealthRequest, ...grpc.CallOption) (*pluginv1.HealthResponse, error) {
	c.healthCalls++
	return &pluginv1.HealthResponse{Healthy: true}, nil
}

func newAccountBindingsManager() (*PluginManager, *accountBindingsRepository, *pluginRuntime) {
	enabledAt := time.Now().Add(-time.Hour)
	installation := &PluginInstallation{
		ID: 7, State: PluginStateEnabled, BinarySHA256: "test-binary", EnabledAt: &enabledAt,
		Bindings: []PluginBinding{{
			PluginID: 7, Capability: PluginCapabilityOpenAIOAuthOutbound,
			Platform: PlatformOpenAI, AccountType: AccountTypeOAuth, Enabled: true, AccountIDs: []int64{10, 12},
		}},
	}
	repo := &accountBindingsRepository{installation: installation}
	runtimeInstallation := *installation
	runtimeInstallation.Bindings = clonePluginBindings(installation.Bindings)
	runtime := &pluginRuntime{
		installation: &runtimeInstallation, client: hcplugin.NewClient(&hcplugin.ClientConfig{}),
		api: &accountBindingsHealthClient{}, done: make(chan struct{}),
	}
	manager := &PluginManager{repo: repo, runtimes: map[int64]*pluginRuntime{7: runtime}}
	manager.route.Store(&pluginRoute{pluginID: 7, runtime: runtime, accountIDs: pluginAccountIDSet([]int64{10, 12})})
	return manager, repo, runtime
}

func TestPluginEnableUpdatesAccountBindingsWithoutRestart(t *testing.T) {
	manager, repo, runtime := newAccountBindingsManager()
	before := manager.route.Load()
	enabledAt := *repo.installation.EnabledAt
	require.True(t, runtime.beginRequest())
	defer runtime.finishRequest()

	result, err := manager.Enable(context.Background(), 7, false, []int64{14, 12, 14})
	require.NoError(t, err)
	require.Equal(t, []int64{12, 14}, bindingAccountIDs(result.Bindings))
	require.Equal(t, enabledAt, *result.EnabledAt)
	require.Equal(t, PluginStateEnabled, result.State)
	require.True(t, result.RuntimeHealthy)
	require.Same(t, runtime, manager.route.Load().runtime)
	require.Same(t, runtime, manager.runtimes[7])
	require.False(t, runtime.draining.Load())
	require.Equal(t, int64(1), runtime.inFlight.Load())
	require.Equal(t, pluginAccountIDSet([]int64{10, 12}), before.accountIDs, "existing requests retain their snapshot")
	require.False(t, manager.ShouldRouteOpenAIOAuth(&Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.True(t, manager.ShouldRouteOpenAIOAuth(&Account{ID: 14, Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	require.NoError(t, manager.reconcileOnce(context.Background()))
	require.Same(t, runtime, manager.route.Load().runtime)
	require.False(t, runtime.draining.Load())
}

func TestPluginEnableKeepsBindingsWhenUpdateFails(t *testing.T) {
	for _, failure := range []string{"validation", "save", "state_changed", "empty"} {
		t.Run(failure, func(t *testing.T) {
			manager, repo, runtime := newAccountBindingsManager()
			before := manager.route.Load()
			ids := []int64{14}
			switch failure {
			case "validation":
				repo.validationErr = errors.New("not an OpenAI OAuth account")
			case "save":
				repo.saveErr = errors.New("database unavailable")
			case "state_changed":
				repo.saveErr = ErrPluginStateChanged
			case "empty":
				ids = nil
			}
			_, err := manager.Enable(context.Background(), 7, false, ids)
			require.Error(t, err)
			require.Same(t, before, manager.route.Load())
			require.Equal(t, []int64{10, 12}, bindingAccountIDs(repo.installation.Bindings))
			require.False(t, runtime.draining.Load())
		})
	}
}

func TestPluginEnableUnchangedAccountBindingsDoesNotWrite(t *testing.T) {
	manager, repo, runtime := newAccountBindingsManager()
	before := manager.route.Load()
	_, err := manager.Enable(context.Background(), 7, false, []int64{12, 10, 12})
	require.NoError(t, err)
	require.Zero(t, repo.saveCalls)
	require.Same(t, before, manager.route.Load())
	require.Same(t, runtime, manager.runtimes[7])
}

func TestPluginReconcileUpdatesAccountBindingsWithoutRestart(t *testing.T) {
	manager, repo, runtime := newAccountBindingsManager()
	before := manager.route.Load()
	// Simulate bindings saved by another host instance.
	repo.installation.Bindings[0].AccountIDs = []int64{12, 14}
	require.NoError(t, manager.reconcileOnce(context.Background()))
	require.Same(t, runtime, manager.route.Load().runtime)
	require.Same(t, runtime, manager.runtimes[7])
	require.False(t, runtime.draining.Load())
	require.Equal(t, pluginAccountIDSet([]int64{12, 14}), manager.route.Load().accountIDs)
	require.Equal(t, pluginAccountIDSet([]int64{10, 12}), before.accountIDs)
	require.Equal(t, 1, runtime.api.(*accountBindingsHealthClient).healthCalls)
	require.Zero(t, repo.saveCalls)
}

func TestPluginBindingUpdatePreservesConcurrentRuntimeFailure(t *testing.T) {
	manager, repo, _ := newAccountBindingsManager()
	repo.onSave = func() {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		delete(manager.runtimes, 7)
		manager.route.Store(&pluginRoute{pluginID: 7, accountIDs: pluginAccountIDSet([]int64{10, 12}), unavailable: "process exited"})
	}
	result, err := manager.Enable(context.Background(), 7, false, []int64{14})
	require.NoError(t, err)
	require.False(t, result.RuntimeHealthy)
	require.Nil(t, manager.route.Load().runtime)
	require.Equal(t, "process exited", manager.route.Load().unavailable)
	require.Equal(t, pluginAccountIDSet([]int64{14}), manager.route.Load().accountIDs)
}
