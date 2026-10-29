package httpapi

import (
	"context"
	"html/template"
	"net/http"
	"path/filepath"
	"time"
)

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "loveservice-api",
		"version": a.version,
		"time":    time.Now().UTC(),
	})
}

func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status := "ready"
	code := http.StatusOK
	dependencies := map[string]string{"postgres": "up", "redis": "degraded"}
	if a.store != nil {
		if err := a.store.Ping(ctx); err != nil {
			status = "not_ready"
			code = http.StatusServiceUnavailable
			dependencies["postgres"] = "down"
		}
	}
	writeJSON(w, code, map[string]any{
		"status":       status,
		"version":      a.version,
		"dependencies": dependencies,
		"time":         time.Now().UTC(),
	})
}

func (a *API) openAPI(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(a.cfg.DocsDir, "openapi.yaml")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}

var swaggerTemplate = template.Must(template.New("swagger").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>LoveService API — Swagger UI</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
<style>
body{margin:0;background:#0f1117}.topbar{display:none}.swagger-ui .info .title{color:#ff4d8d}
</style>
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js" crossorigin></script>
<script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-standalone-preset.js" crossorigin></script>
<script>
window.onload=function(){
  SwaggerUIBundle({
    url:'/openapi.yaml',
    dom_id:'#swagger-ui',
    deepLinking:true,
    persistAuthorization:true,
    displayRequestDuration:true,
    tryItOutEnabled:true,
    presets:[SwaggerUIBundle.presets.apis,SwaggerUIStandalonePreset],
    plugins:[SwaggerUIBundle.plugins.DownloadUrl],
    layout:'StandaloneLayout'
  });
};
</script>
</body>
</html>`))

func (a *API) swaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = swaggerTemplate.Execute(w, nil)
}
