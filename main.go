package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

const (
	port = "8080"
)

type apiConfig struct {
	fileserverHits atomic.Int32
}

func main() {
	mux := http.NewServeMux()
	apiCfg := apiConfig{
		fileserverHits: atomic.Int32{},
	}

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(handler()))

	mux.HandleFunc("/healthz", endPointHandler)
	mux.HandleFunc("/metrics", apiCfg.writeNumberRequest)
	mux.HandleFunc("/reset", apiCfg.resetHits)

	server := http.Server{
		Handler: mux,
		Addr:    ":" + port,
	}

	server.ListenAndServe()

}

func endPointHandler(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", "text/plain; charset=utf-8")
	res.WriteHeader(200)
	res.Write([]byte("OK"))

}

func handler() http.Handler {
	return http.StripPrefix("/app", http.FileServer(http.Dir(".")))
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(res, req)
	})
}

func (cfg *apiConfig) writeNumberRequest(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", "text/plain; charset=utf-8")
	res.WriteHeader(200)
	res.Write([]byte(fmt.Sprintf("Hits: %d\n", cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) resetHits(res http.ResponseWriter, req *http.Request) {
	cfg.fileserverHits.Store(0)
	res.WriteHeader(200)
	res.Write([]byte("OK"))
}
