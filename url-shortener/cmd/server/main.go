package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"url-shortener/internal/httpapi"
	"url-shortener/internal/shortener"
	"url-shortener/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "server address")
	base := flag.String("base", "http://localhost:8080", "base url")
	storeType := flag.String("store", "memory", "store type: memory or file")
	dataPath := flag.String("data", "data/links.jsonl", "persistent store file")
	flag.Parse()

	var app *shortener.Service

	if *storeType == "file" {
		db, err := store.NewFile(*dataPath)
		if err != nil {
			log.Fatal("could not open persistent store:", err)
		}
		defer db.Close()

		app = shortener.NewService(db)
	} else if *storeType == "memory" {
		db := store.New()
		app = shortener.NewService(db)
	} else {
		log.Fatal("unknown store type:", *storeType)
	}

	router := httpapi.NewHandler(app, *base)

	server := http.Server{
		Addr:              *addr,
		Handler:           router,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Println("server started on", *addr)
	log.Fatal(server.ListenAndServe())
}
