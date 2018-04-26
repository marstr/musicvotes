package actions

import (
	"encoding/json"
	"net/http"

	egdp "github.com/Azure/azure-sdk-for-go/services/eventgrid/2018-01-01/eventgrid"
	"github.com/Azure/buffalo-azure/sdk/eventgrid"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/buffalo/middleware"
	"github.com/gobuffalo/buffalo/middleware/csrf"
	"github.com/gobuffalo/buffalo/middleware/i18n"
	"github.com/gobuffalo/buffalo/middleware/ssl"
	"github.com/gobuffalo/envy"
	"github.com/gobuffalo/packr"
	"github.com/marstr/musicvotes/models"
	"github.com/unrolled/secure"
)

// ENV is used to help switch settings based on where the
// application is being run. Default is "development".
var ENV = envy.Get("GO_ENV", "development")
var app *buffalo.App
var T *i18n.Translator

// App is where all routes and middleware for buffalo
// should be defined. This is the nerve center of your
// application.
func App() *buffalo.App {
	if app == nil {
		app = buffalo.New(buffalo.Options{
			Env:         ENV,
			SessionName: "_musicvotes_session",
		})
		// Automatically redirect to SSL
		app.Use(ssl.ForceSSL(secure.Options{
			SSLRedirect:     ENV == "production",
			SSLProxyHeaders: map[string]string{"X-Forwarded-Proto": "https"},
		}))

		if ENV == "development" {
			app.Use(middleware.ParameterLogger)
		}

		// Protect against CSRF attacks. https://www.owasp.org/index.php/Cross-Site_Request_Forgery_(CSRF)
		// Remove to disable this.
		app.Use(csrf.New)

		// Wraps each request in a transaction.
		//  c.Value("tx").(*pop.PopTransaction)
		// Remove to disable this.
		app.Use(middleware.PopTransaction(models.DB))

		// Setup and use translations:
		var err error
		if T, err = i18n.New(packr.NewBox("../locales"), "en-US"); err != nil {
			app.Stop(err)
		}
		app.Use(T.Middleware())

		app.GET("/", HomeHandler)

		tds := eventgrid.NewTypeDispatchSubscriber(&eventgrid.BaseSubscriber{})

		tds.Bind(
			"Microsoft.Storage.BlobCreated",
			func(c buffalo.Context, e eventgrid.Event) error {
				c.Logger().Debug("Entering anonymous BlobCreated decoder")
				var payload egdp.StorageBlobCreatedEventData
				ingressCache.Add(e)
				if err := json.Unmarshal(e.Data, &payload); err != nil {
					return c.Error(http.StatusBadRequest, err)
				}
				return IngressBlobCreated(c, e, payload)
			})

		app.POST("/ingress", tds.Receive)
		app.GET("/ingress", IngressListEvents)
		app.GET("/ingress/{event_id}", IngressShowEvent)

		app.Resource("/songs", SongsResource{})
		app.ServeFiles("/", assetsBox) // serve files from the public directory
	}

	return app
}
