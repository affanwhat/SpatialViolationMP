package main

import (
	"log"
	"net/http"
)

func main() {
	// 1. Create a file server that looks in the current directory (".")
	fileServer := http.FileServer(http.Dir("."))

	// 2. Tell Go to use this file server for all requests
	// It will automatically find index.html, app.js, and data/provinces.json
	http.Handle("/", fileServer)

	log.Println("Server starting at http://localhost:8081")

	// 3. Start the server
	err := http.ListenAndServe(":8081", nil)
	if err != nil {
		log.Fatal(err)
	}
}
