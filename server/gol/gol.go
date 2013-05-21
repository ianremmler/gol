package main

import (
	"github.com/ianremmler/gol"
	"code.google.com/p/go.net/websocket"

	"go/build"
	"math/rand"
	"net/http"
	"os"
	"time"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	gol := gol.NewGol()
	gol.Run()

	clientDir := build.Default.GOPATH + "/src/github.com/ianremmler/gol/client"
	http.Handle("/gol/", websocket.Handler(gol.WSHandler))
	http.Handle("/", http.FileServer(http.Dir(clientDir)))
	port := ":8000"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}
	if err := http.ListenAndServe(port, nil); err != nil {
		panic("ListenAndServe: " + err.Error())
	}
}
