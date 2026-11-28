//go:build embed

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSEOTestServer(t *testing.T, provider PublicSettingsProvider) *FrontendServer {
	t.Helper()
	distFS := fstest.MapFS{
		"index.html":      {Data: []byte(`<html><head><title>Default</title></head><body><div id="app"></div></body></html>`)},
		"_seo/index.html": {Data: []byte(`<html><head><title>Home SEO</title></head><body><div id="app"><h1>home</h1></div></body></html>`)},
		"_seo/faq.html":   {Data: []byte(`<html><head><title>FAQ SEO</title><link rel="canonical" href="https://aixlau.me/faq" /></head><body><div id="app"><h1>faq</h1></div></body></html>`)},
		"_seo/notes.txt":  {Data: []byte("ignored")},
		"assets/app.js":   {Data: []byte("console.log(1)")},
	}
	baseHTML, err := distFS.ReadFile("index.html")
	require.NoError(t, err)
	cache := NewHTMLCache()
	cache.SetBaseHTML(baseHTML)
	seoPages := loadSEOPages(distFS)
	seoCaches := map[string]*HTMLCache{}
	for route, html := range seoPages {
		c := NewHTMLCache()
		c.SetBaseHTML(html)
		seoCaches[route] = c
	}
	return &FrontendServer{
		distFS:     distFS,
		fileServer: http.FileServer(http.FS(distFS)),
		baseHTML:   baseHTML,
		cache:      cache,
		settings:   provider,
		seoPages:   seoPages,
		seoCaches:  seoCaches,
	}
}

func serveSEOPath(t *testing.T, server *FrontendServer, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(server.Middleware())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestLoadSEOPages(t *testing.T) {
	server := newSEOTestServer(t, &mockSettingsProvider{settings: map[string]string{}})
	assert.Len(t, server.seoPages, 2)
	assert.Contains(t, server.seoPages, "/")
	assert.Contains(t, server.seoPages, "/faq")
}

func TestServeIndexHTMLUsesRouteSEOTemplate(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]string{"site_name": "Brand"}}
	server := newSEOTestServer(t, provider)

	for _, path := range []string{"/faq", "/faq/"} {
		w := serveSEOPath(t, server, path)
		require.Equal(t, http.StatusOK, w.Code, path)
		body := w.Body.String()
		assert.Contains(t, body, "<title>FAQ SEO</title>", "route title must not be replaced by site name")
		assert.Contains(t, body, `<link rel="canonical" href="https://aixlau.me/faq" />`)
		assert.Contains(t, body, "<h1>faq</h1>")
		assert.Contains(t, body, "window.__APP_CONFIG__=")
	}

	home := serveSEOPath(t, server, "/").Body.String()
	assert.Contains(t, home, "<title>Home SEO</title>")

	// Routes without a template keep the generic SPA shell and branded title.
	other := serveSEOPath(t, server, "/dashboard").Body.String()
	assert.Contains(t, other, "<title>Brand - AI API Gateway</title>")
	assert.NotContains(t, other, "<h1>")
}

func TestSEOTemplatesAreNotServedDirectly(t *testing.T) {
	server := newSEOTestServer(t, &mockSettingsProvider{settings: map[string]string{}})
	w := serveSEOPath(t, server, "/_seo/faq.html")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "<title>Default</title>")
	assert.NotContains(t, w.Body.String(), "FAQ SEO")
}

func TestInvalidateCacheClearsSEOCaches(t *testing.T) {
	provider := &mockSettingsProvider{settings: map[string]string{}}
	server := newSEOTestServer(t, provider)

	serveSEOPath(t, server, "/faq")
	serveSEOPath(t, server, "/faq")
	assert.Equal(t, 1, provider.called)

	server.InvalidateCache()
	serveSEOPath(t, server, "/faq")
	assert.Equal(t, 2, provider.called)
}
