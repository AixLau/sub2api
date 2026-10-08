package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountRPMCacheStub struct {
	service.RPMCache
	counts map[int64]int
	ids    []int64
	err    error
}

func (s *accountRPMCacheStub) GetRecentRPMBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	s.ids = append([]int64(nil), ids...)
	result := map[int64]int{}
	for _, id := range ids {
		result[id] = s.counts[id]
	}
	return result, s.err
}
func (s *accountRPMCacheStub) GetRPM(_ context.Context, id int64) (int, error) {
	return s.counts[id], s.err
}

func TestAccountRPMBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body string
		err        error
		status     int
	}{
		{"counts", `{"account_ids":[42,43,42]}`, nil, 200},
		{"empty", `{"account_ids":[]}`, nil, 400},
		{"negative", `{"account_ids":[-1]}`, nil, 400},
		{"zero", `{"account_ids":[0]}`, nil, 400},
		{"fraction", `{"account_ids":[1.5]}`, nil, 400},
		{"too_many", `{"account_ids":[` + strings.Repeat("42,", 1000) + `42]}`, nil, 400},
		{"redis_failure", `{"account_ids":[42]}`, errors.New("redis failed"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &accountRPMCacheStub{counts: map[int64]int{42: 7}, err: tc.err}
			h := &AccountHandler{rpmCache: cache}
			router := gin.New()
			router.POST("/rpm", h.GetRPM)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/rpm", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			if tc.status == 200 {
				require.JSONEq(t, `{"code":0,"message":"success","data":{"rpm":{"42":7,"43":0}}}`, rec.Body.String())
				require.Equal(t, []int64{42, 43}, cache.ids)
			}
			if tc.status == 503 {
				require.NotContains(t, rec.Body.String(), `"rpm"`)
			}
		})
	}
}

func TestAccountRPMListAndDetailAllPlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, lite := range []string{"0", "1"} {
		t.Run("lite="+lite, func(t *testing.T) {
			svc := newStubAdminService()
			svc.accounts = []service.Account{
				{ID: 42, Name: "OpenAI", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth},
				{ID: 43, Name: "Gemini", Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey},
				{ID: 44, Name: "Anthropic", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth},
			}
			cache := &accountRPMCacheStub{counts: map[int64]int{42: 7}}
			h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, cache, nil)
			router := gin.New()
			router.GET("/accounts", h.List)
			fetch := func(etag string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("GET", "/accounts?lite="+lite+"&include_scheduler_score=0", nil)
				req.Header.Set("If-None-Match", etag)
				router.ServeHTTP(rec, req)
				return rec
			}
			first := fetch("")
			require.Equal(t, 200, first.Code)
			var payload struct {
				Data struct {
					Items []struct {
						ID  int64 `json:"id"`
						RPM *int  `json:"current_rpm"`
					} `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(first.Body.Bytes(), &payload))
			require.Len(t, payload.Data.Items, 3)
			for _, item := range payload.Data.Items {
				require.NotNil(t, item.RPM)
				require.Equal(t, cache.counts[item.ID], *item.RPM)
			}
			require.Equal(t, http.StatusNotModified, fetch(first.Header().Get("ETag")).Code)
			cache.counts[42]++
			changed := fetch(first.Header().Get("ETag"))
			require.Equal(t, 200, changed.Code)
			require.NotEqual(t, first.Header().Get("ETag"), changed.Header().Get("ETag"))
			for _, account := range svc.accounts {
				detail := h.buildAccountResponseWithRuntime(context.Background(), &account)
				require.NotNil(t, detail.CurrentRPM, fmt.Sprint(account.ID))
			}
			cache.err = errors.New("unavailable")
			cache.counts = nil
			failed := fetch("")
			require.Equal(t, 200, failed.Code)
			require.NotContains(t, failed.Body.String(), `"current_rpm"`)
		})
	}
}
