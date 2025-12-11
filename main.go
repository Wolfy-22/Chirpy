package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wolfy-22/Chirpy.git/internal/database"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

const (
	port = "8080"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Printf("Error connecting to database: %s", err)
		os.Exit(1)
	}
	dbQ := database.New(db)

	mux := http.NewServeMux()
	apiCfg := apiConfig{
		fileserverHits: atomic.Int32{},
		dbQueries:      dbQ,
		platform:       platform,
	}

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(handler()))

	mux.HandleFunc("GET /api/healthz", endPointHandler)
	mux.HandleFunc("POST /api/chirps", apiCfg.createChirp)
	mux.HandleFunc("POST /api/users", apiCfg.addUser)
	mux.HandleFunc("GET /admin/metrics", apiCfg.writeNumberRequest)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetAll)
	mux.HandleFunc("GET /api/chirps", apiCfg.getChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirp)

	server := http.Server{
		Handler: mux,
		Addr:    ":" + port,
	}

	server.ListenAndServe()

}

func (cfg *apiConfig) getChirp(res http.ResponseWriter, req *http.Request) {
	type Chirp struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Body      string    `json:"body"`
		UserID    uuid.UUID `json:"user_id"`
	}

	id, err := uuid.Parse(req.PathValue("chirpID"))
	if err != nil {
		log.Printf("Error getting ID: %s", err)
		res.WriteHeader(404)
		return
	}
	chirp, err := cfg.dbQueries.GetChirpByID(req.Context(), id)
	if err != nil {
		log.Printf("Error getting chirps: %s", err)
		res.WriteHeader(404)
		return
	}

	fixedChirp := Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	}

	dat, err := json.Marshal(fixedChirp)
	if err != nil {
		log.Printf("Error marshalling json: %s", err)
		res.WriteHeader(404)
		return
	}

	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(200)
	res.Write(dat)
}

func (cfg *apiConfig) getChirps(res http.ResponseWriter, req *http.Request) {
	type Chirp struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Body      string    `json:"body"`
		UserID    uuid.UUID `json:"user_id"`
	}

	chirps, err := cfg.dbQueries.GetAllChirps(req.Context())
	if err != nil {
		log.Printf("Error getting chirps: %s", err)
		res.WriteHeader(400)
		return
	}
	var fixedChirps []Chirp
	var fixedChrip Chirp
	for _, chirp := range chirps {
		fixedChrip = Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		}
		fixedChirps = append(fixedChirps, fixedChrip)
	}

	dat, err := json.Marshal(fixedChirps)
	if err != nil {
		log.Printf("Error marshalling json: %s", err)
		res.WriteHeader(400)
		return
	}

	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(200)
	res.Write(dat)
}

func (cfg *apiConfig) addUser(res http.ResponseWriter, req *http.Request) {
	type userData struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
	}

	reqEmail := req.Body
	decoder := json.NewDecoder(reqEmail)
	Data := userData{}
	err := decoder.Decode(&Data)
	if err != nil {
		log.Printf("Error decoding request body: %s", err)
		res.WriteHeader(400)
		return
	}

	user, err := cfg.dbQueries.CreateUser(req.Context(), Data.Email)
	if err != nil {
		log.Printf("Error creating user: %s", err)
		res.WriteHeader(400)
		return
	}

	respBody := userData{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	}

	dat, err := json.Marshal(respBody)
	if err != nil {
		log.Printf("Error marhalling JSON: %s", err)
		res.WriteHeader(500)
		return
	}

	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(201)
	res.Write(dat)

}

func (cfg *apiConfig) createChirp(res http.ResponseWriter, req *http.Request) {
	type Chirp struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Body      string    `json:"body"`
		UserID    uuid.UUID `json:"user_id"`
		Error     string    `json:"error"`
		Valid     bool      `json:"valid"`
	}

	chirpBody := ""
	chirpError := ""
	chirpValid := true

	Body := req.Body
	decoder := json.NewDecoder(Body)
	chirp := Chirp{}
	err := decoder.Decode(&chirp)
	if err != nil {
		log.Printf("Error decoding chirp: %s", err)
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

	CHIRP, err := cfg.dbQueries.CreateChirp(req.Context(), database.CreateChirpParams{
		Body:   cleaned,
		UserID: chirp.UserID,
	})
	if err != nil {
		log.Printf("Error creating chrip: %s", err)
		res.WriteHeader(400)
		return
	}

	respBody := Chirp{
		ID:        CHIRP.ID,
		CreatedAt: CHIRP.CreatedAt,
		UpdatedAt: CHIRP.UpdatedAt,
		Body:      CHIRP.Body,
		UserID:    CHIRP.UserID,
		Error:     chirpError,
		Valid:     chirpValid,
	}

	dat, err := json.Marshal(respBody)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		res.WriteHeader(500)
		return
	}
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(201)
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
	res.Write([]byte(fmt.Sprintf(`<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>`, cfg.fileserverHits.Load())))
}

func (cfg *apiConfig) resetAll(res http.ResponseWriter, req *http.Request) {
	if cfg.platform != "dev" {
		res.WriteHeader(403)
		res.Write([]byte("Forbidden\n"))
		return
	}
	cfg.fileserverHits.Store(0)
	err := cfg.dbQueries.DeleteUsers(req.Context())
	if err != nil {
		log.Printf("Error deleting users: %s", err)
		res.WriteHeader(400)
		res.Write([]byte("error\n"))
		return
	}
	res.WriteHeader(200)
	res.Write([]byte("OK\n"))
}
