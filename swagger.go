package zentrox

import (
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"
)

// SwaggerHandler returns a handler that serves Swagger UI
//
// Usage:
//
//	app.GET("/swagger/*any", zentrox.SwaggerHandler())
//
// Or with custom config:
//
//	app.GET("/swagger/*any", zentrox.SwaggerHandler(
//	    httpSwagger.URL("http://localhost:8000/swagger/doc.json"),
//	))
func SwaggerHandler(options ...func(*httpSwagger.Config)) Handler {
	handler := httpSwagger.Handler(options...)

	return func(c *Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	}
}

// ServeSwagger mounts Swagger UI at the specified path
// This is a convenience method that automatically sets up the route
//
// Usage:
//
//	app.ServeSwagger("/swagger")
//	// Now accessible at: http://localhost:8000/swagger/index.html
func (a *App) ServeSwagger(path string, options ...func(*httpSwagger.Config)) *App {
	if path == "" {
		path = "/swagger"
	}

	// Ensure path ends with /*filepath for wildcard matching
	swaggerPath := path
	if swaggerPath[len(swaggerPath)-1] != '/' {
		swaggerPath += "/"
	}
	swaggerPath += "*filepath"

	a.GET(swaggerPath, SwaggerHandler(options...))

	return a
}

// SwaggerJSON serves the raw swagger.json file
// Useful if you want to expose the OpenAPI spec separately
//
// Usage:
//
//	app.GET("/swagger.json", zentrox.SwaggerJSON())
func SwaggerJSON() Handler {
	return func(c *Context) {
		// Serve the generated swagger.json from docs package
		http.ServeFile(c.Writer, c.Request, "./docs/swagger.json")
	}
}

// SwaggerYAML serves the raw swagger.yaml file
//
// Usage:
//
//	app.GET("/swagger.yaml", zentrox.SwaggerYAML())
func SwaggerYAML() Handler {
	return func(c *Context) {
		// Serve the generated swagger.yaml from docs package
		http.ServeFile(c.Writer, c.Request, "./docs/swagger.yaml")
	}
}
