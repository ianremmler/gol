package main

//go:generate esc -prefix static -o static.go static

import (
	"github.com/ianremmler/gol"

	"log"
	"math/rand"
	"net/http"
	"os"
	"time"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	g := gol.New()
	g.Run()

	http.Handle("/gol/", g)
	http.Handle("/", http.FileServer(FS(false)))
	port := ":8000"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}
	log.Fatal(http.ListenAndServe(port, nil))
}
