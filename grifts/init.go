package grifts

import (
	"github.com/gobuffalo/buffalo"
	"github.com/marstr/musicvotes/actions"
)

func init() {
	buffalo.Grifts(actions.App())
}
