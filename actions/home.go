package actions

import "github.com/gobuffalo/buffalo"

// HomeHandler is a default handler to serve up
// a home page.
func HomeHandler(c buffalo.Context) error {
	if commitID == "" {
		c.Data()["commitID"] = "Unknown Revision"
	} else {
		c.Data()["commitID"] = commitID
	}
	return c.Render(200, r.HTML("index.html"))
}
