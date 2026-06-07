// A tiny HTTP server used as the sample workload the deploy pipeline clones,
// builds, and ships. It listens on $PORT (default 8080), greets on "/", and
// exposes "/healthz" for probes. Configuration is read from the environment so
// it demonstrates the generated ConfigMap/Secret being wired in via envFrom.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := getenv("PORT", "8080")
	greeting := getenv("GREETING", "Hello from vibsl")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s — served by %s\n", greeting, hostname())
	})

	addr := ":" + port
	log.Printf("sample-app listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
