package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

type Book struct {
	ID    int    `xml:"id"`
	Title string `xml:"title"`
}

func main() {
	app := zentrox.NewApp()
	app.Plug(middleware.Recovery(), middleware.Logger())

	app.OnGet("/html", func(c *zentrox.Context) {
		c.SendHTML(http.StatusOK, "<h1>Hello <em>zentrox</em></h1>")
	})

	app.OnGet("/xml", func(c *zentrox.Context) {
		c.SendXML(http.StatusOK, Book{ID: 1, Title: "Golang"})
	})

	app.OnGet("/file", func(c *zentrox.Context) {
		path := "demo.txt"
		_ = os.WriteFile(path, []byte("zentrox file download"), 0644)
		c.SendAttachment(path, "zentrox-demo.txt")
	})

	app.OnGet("/stream", func(c *zentrox.Context) {
		c.PushStream(func(w io.Writer, flush func()) {
			for i := 1; i <= 5; i++ {
				fmt.Fprintf(w, "chunk %d\n", i)
				flush()
				time.Sleep(200 * time.Millisecond)
			}
		})
	})

	app.OnGet("/sse", func(c *zentrox.Context) {
		c.PushSSE(func(event func(name, data string)) {
			for i := 1; i <= 3; i++ {
				event("tick", fmt.Sprintf("%d", i))
				time.Sleep(300 * time.Millisecond)
			}
		})
	})

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
