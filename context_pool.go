package zentrox

import (
	"net/http"
	"sync"
)

// Context pooling
var ctxPool = sync.Pool{
	New: func() any {
		return &Context{
			params: map[string]string{},
			store:  make(map[any]any),
			index:  -1,
		}
	},
}

func acquireContext(w http.ResponseWriter, r *http.Request) *Context {
	c := ctxPool.Get().(*Context)
	c.Writer = w
	c.Request = r
	c.index = -1
	c.aborted = false
	c.err = nil
	c.realIP = nil
	c.route = ""
	c.responseCommitted = false
	c.validator = nil
	c.jsonCodec = nil
	return c
}

func releaseContext(c *Context) {
	for k := range c.params {
		delete(c.params, k)
	}
	for k := range c.store {
		delete(c.store, k)
	}
	c.Writer = nil
	c.Request = nil
	c.stack = nil
	c.err = nil
	c.aborted = false
	c.index = -1
	c.realIP = nil
	c.route = ""
	c.responseCommitted = false
	c.validator = nil
	c.jsonCodec = nil

	ctxPool.Put(c)
}
