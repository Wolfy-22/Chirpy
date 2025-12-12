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

	"github.com/Wolfy-22/Chirpy.git/internal/auth"
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
	secret         string
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")
	secret := os.Getenv("SECRET")
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
		secret:         secret,
	}

	mux.Handle("/app/", apiCfg.middlewareMetricsInc(handler()))

	mux.HandleFunc("GET /api/healthz", endPointHandler)
	mux.HandleFunc("POST /api/chirps", apiCfg.createChirp)
	mux.HandleFunc("POST /api/users", apiCfg.addUser)
	mux.HandleFunc("GET /admin/metrics", apiCfg.writeNumberRequest)
	mux.HandleFunc("POST /admin/reset", apiCfg.resetAll)
	mux.HandleFunc("GET /api/chirps", apiCfg.getChirps)
	mux.HandleFunc("GET /api/chirps/{chirpID}", apiCfg.getChirp)
	mux.HandleFunc("POST /api/login", apiCfg.login)

	server := http.Server{
		Handler: mux,
		Addr:    ":" + port,
	}

	server.ListenAndServe()

}

func (cfg *apiConfig) login(res http.ResponseWriter, req *http.Request) {
	type userData struct {
		Email     string `json:"email"`
		Password  string `json:"password"`
		ExpiresIn int    `json:"expire_in_seconds"`
	}

	type loginResponse struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
		Token     string    `json:"token"`
	}

	decoder := json.NewDecoder(req.Body)
	Data := userData{}
	err := decoder.Decode(&Data)
	if err != nil {
		log.Printf("Error decoding request body: %s", err)
		res.WriteHeader(400)
		return
	}

	user, err := cfg.dbQueries.GetUserByEmail(req.Context(), Data.Email)
	if err != nil {
		log.Printf("Error getting user: %s", err)
		res.WriteHeader(http.StatusUnauthorized)
		res.Write([]byte("incorrect email or password"))

		return
	}

	match, err := auth.CheckPasswordHash(Data.Password, user.HashedPassword)
	if err != nil || !match {
		log.Printf("Error checking password: %s", err)
		res.WriteHeader(http.StatusUnauthorized)
		res.Write([]byte("incorrect email or password"))

		return
	}

	duration := time.Duration(0)
	if Data.ExpiresIn == 0 {
		duration = time.Hour
	} else {
		duration = time.Duration(Data.ExpiresIn)
	}

	token, err := auth.MakeJWT(user.ID, cfg.secret, duration)
	if err != nil {
		log.Printf("Error genrating token: %v", err)
		res.WriteHeader(400)
		return
	}

	respBody := loginResponse{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
		Token:     token,
	}

	dat, err := json.Marshal(respBody)
	if err != nil {
		log.Printf("Error marhalling JSON: %s", err)
		res.WriteHeader(500)
		return
	}

	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(200)
	res.Write(dat)
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
	type userParams struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	type userData struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
		Password  string    `json:"hashed_password"`
	}

	decoder := json.NewDecoder(req.Body)
	Data := userParams{}
	err := decoder.Decode(&Data)
	if err != nil {
		log.Printf("Error decoding request body: %s", err)
		res.WriteHeader(400)
		return
	}

	hashedPassword, err := auth.HashPassword(Data.Password)
	if err != nil {
		log.Printf("Error hashing password: %s", err)
		res.WriteHeader(400)
		return
	}

	user, err := cfg.dbQueries.CreateUser(req.Context(), database.CreateUserParams{
		Email:          Data.Email,
		HashedPassword: hashedPassword,
	})
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
		return
	}

	token, err := auth.GetBearerToken(req.Header)
	if err != nil {
		log.Printf("Error getting token: %v", err)
		res.WriteHeader(http.StatusUnauthorized)
		return
	}

	userId, err := auth.ValidateJWT(token, cfg.secret)
	if err != nil {
		log.Printf("Error getting token: %v", err)
		res.WriteHeader(http.StatusUnauthorized)
		return
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
		UserID: userId,
	})
	if err != nil {
		log.Printf("Error creating chrip: %s", err)
		res.WriteHeader(400)
		return
	}

	respBody := Chirp{
		ID:        userId,
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
