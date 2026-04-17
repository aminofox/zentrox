package middleware

import (
	"log"
	"net/http"

	"github.com/aminofox/zentrox"
)

func Recovery() zentrox.Handler {
	return func(c *zentrox.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic: %v", r)
				c.JSON(http.StatusInternalServerError, zentrox.HTTPError{
					Code:    http.StatusInternalServerError,
					Message: zentrox.MsgInternalServerError,
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}
