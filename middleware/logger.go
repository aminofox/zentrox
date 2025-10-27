package middleware

import (
	"log"
	"time"

	"github.com/aminofox/zentrox"
)

type LogFunc func(method, path string, status int, duration time.Duration, err error)

func Logger() zentrox.Handler {
	return LoggerWithFunc(nil)
}

func LoggerWithFunc(fn LogFunc) zentrox.Handler {
	if fn == nil {
		fn = func(method, path string, status int, duration time.Duration, err error) {
			log.Printf("%s %s %d %v", method, path, status, duration)
		}
	}

	return func(c *zentrox.Context) {
		start := time.Now()
		c.Next()

		status := 200
		if rw, ok := c.Writer.(interface{ Status() int }); ok {
			if s := rw.Status(); s != 0 {
				status = s
			}
		}

		fn(c.Request.Method, c.Request.URL.Path, status, time.Since(start), c.Error())
	}
}
