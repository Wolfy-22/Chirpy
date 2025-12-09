package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
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

	mux.HandleFunc("GET /api/healthz", endPointHandler)
	mux.HandleFunc("POST /api/validate_chirp", validateChirp)
	mux.HandleFunc("GET /admin/metrics", apiCfg.writeNumberRequest)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetHits)

	server := http.Server{
		Handler: mux,
		Addr:    ":" + port,
	}

	server.ListenAndServe()

}

func validateChirp(res http.ResponseWriter, req *http.Request) {

	type JSONChirp struct {
		Body        string `json:"body"`
		Error       string `json:"error"`
		Valid       bool   `json:"valid"`
		CleanedBody string `json:"cleaned_body"`
	}

	chirpBody := ""
	chirpError := ""
	chirpValid := true

	body := req.Body
	decoder := json.NewDecoder(body)
	chirp := JSONChirp{}
	err := decoder.Decode(&chirp)
	if err != nil {
		log.Printf("Error decoding chirp: %w", err)
		res.WriteHeader(400)
		chirpBody = ""
		chirpError = "Something went wrong"
		chirpValid = false
	}

	if len(chirp.Body) > 140 {
		chirpBody = ""
		chirpError = "Chirp is too long"
		chirpValid = false
		res.WriteHeader(400)
	} else {
		chirpBody = chirp.Body
	}

	splitBody := strings.Split(chirpBody, " ")
	var bodysplice []string
	for _, word := range splitBody {
		if strings.ToLower(word) == "kerfuffle" || strings.ToLower(word) == "sharbert" || strings.ToLower(word) == "fornax" {
			word = "****"
		}
		bodysplice = append(bodysplice, word)
	}
	cleaned := strings.Join(bodysplice, " ")

	respBody := JSONChirp{
		Body:        chirpBody,
		Error:       chirpError,
		Valid:       chirpValid,
		CleanedBody: cleaned,
	}

	dat, err := json.Marshal(respBody)
	if err != nil {
		log.Printf("Error marshalling JSON: %w", err)
		res.WriteHeader(500)
	}
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(200)
	res.Write(dat)

}

func endPointHandler(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", "text/plain; charset=utf-8")
	res.WriteHeader(200)
	res.Write([]byte("OK\n"))

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
	res.Header().Add("Content-Type", "text/html")
	res.WriteHeader(http.StatusOK)
	res.Write([]byte(fmt.Sprintf(`
<html>

<body>
	<h1>Welcome, Chirpy Admin</h1>
	<p>Chirpy has been visited %d times!</p>
</body>

</html>
	`, cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) resetHits(res http.ResponseWriter, req *http.Request) {
	cfg.fileserverHits.Store(0)
	res.WriteHeader(200)
	res.Write([]byte("OK\n"))
}
