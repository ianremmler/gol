package main

import (
	"github.com/ianremmler/soc"
	"code.google.com/p/go.net/websocket"

	"go/build"
	"math/rand"
	"net/http"
	"os"
	"time"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	soc := soc.NewSoc()
	soc.Run()

	clientDir := build.Default.GOPATH + "/src/github.com/ianremmler/soc/client"
	http.Handle("/soc/", websocket.Handler(soc.WSHandler))
	http.Handle("/", http.FileServer(http.Dir(clientDir)))
	port := ":8000"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}
	if err := http.ListenAndServe(port, nil); err != nil {
		panic("ListenAndServe: " + err.Error())
	}
}
