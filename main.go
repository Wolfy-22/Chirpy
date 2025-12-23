package main

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	mux.HandleFunc("POST /api/refresh", apiCfg.refresh)
	mux.HandleFunc("POST /api/revoke", apiCfg.revokeToken)
	mux.HandleFunc("PUT /api/users", apiCfg.updateUser)

	server := http.Server{
		Handler: mux,
		Addr:    ":" + port,
	}

	server.ListenAndServe()

}

func (cfg *apiConfig) updateUser(res http.ResponseWriter, req *http.Request) {
	accessToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(res, http.StatusUnauthorized, "Couldn't find token", err)
		return
	}

	userID, err := auth.ValidateJWT(accessToken, cfg.secret)
	if err != nil {
		respondWithError(res, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	type newUserDate struct {
		NewEmail    string `json:"email"`
		NewPassword string `json:"password"`
	}

	type updateResponse struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
	}

	decoder := json.NewDecoder(req.Body)
	params := newUserDate{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(res, http.StatusInternalServerError, "Couldn't decode parameters", err)
		return
	}

	hashedPassword, err := auth.HashPassword(params.NewPassword)
	if err != nil {
		log.Printf("Error hashing password: %s", err)
		res.WriteHeader(400)
		return
	}

	user, err := cfg.dbQueries.UpdateUserEmailAndPasswordByUserID(req.Context(), database.UpdateUserEmailAndPasswordByUserIDParams{
		Email:          params.NewEmail,
		HashedPassword: hashedPassword,
		ID:             userID,
	})
	if err != nil {
		respondWithError(res, http.StatusInternalServerError, "Couldn't update user", err)
		return
	}

	respondWithJSON(res, 200, updateResponse{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     user.Email,
	})

}

func (cfg *apiConfig) createChirp(res http.ResponseWriter, req *http.Request) {
	type Chirp struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		UserID    uuid.UUID `json:"user_id"`
		Body      string    `json:"body"`
	}

	type parameters struct {
		Body string `json:"body"`
	}

	token, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(res, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.secret)
	if err != nil {
		respondWithError(res, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	decoder := json.NewDecoder(req.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		respondWithError(res, http.StatusInternalServerError, "Couldn't decode parameters", err)
		return
	}

	cleaned, err := validateChirp(params.Body)
	if err != nil {
		respondWithError(res, http.StatusBadRequest, err.Error(), err)
		return
	}

	chirp, err := cfg.dbQueries.CreateChirp(req.Context(), database.CreateChirpParams{
		Body:   cleaned,
		UserID: userID,
	})
	if err != nil {
		respondWithError(res, http.StatusInternalServerError, "Couldn't create chirp", err)
		return
	}

	respondWithJSON(res, http.StatusCreated, Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	})
}

func (cfg *apiConfig) revokeToken(res http.ResponseWriter, req *http.Request) {
	refreshToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(res, http.StatusBadRequest, "Couldn't find token", err)
		return
	}

	_, err = cfg.dbQueries.RevokeRefreshToken(req.Context(), refreshToken)
	if err != nil {
		respondWithError(res, http.StatusInternalServerError, "Couldn't revoke session", err)
		return
	}

	res.WriteHeader(http.StatusNoContent)
}

func (cfg *apiConfig) refresh(res http.ResponseWriter, req *http.Request) {
	type refreshResponse struct {
		Token string `json:"token"`
	}

	refreshToken, err := auth.GetBearerToken(req.Header)
	if err != nil {
		respondWithError(res, http.StatusBadRequest, "Couldn't find token", err)
		return
	}

	user, err := cfg.dbQueries.GetUserFromRefreshToken(req.Context(), refreshToken)
	if err != nil {
		respondWithError(res, http.StatusUnauthorized, "Couldn't get user from refresh token", err)
		return
	}

	accessToken, err := auth.MakeJWT(
		user.ID,
		cfg.secret,
		time.Hour,
	)
	if err != nil {
		respondWithError(res, http.StatusUnauthorized, "Couldn't validate token", err)
		return
	}

	respondWithJSON(res, http.StatusOK, refreshResponse{
		Token: accessToken,
	})
}

func (cfg *apiConfig) login(res http.ResponseWriter, req *http.Request) {
	type userData struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	type loginResponse struct {
		ID           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		RefreshToken string    `json:"refresh_token"`
		AccessToken  string    `json:"token"`
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

	access_token, err := auth.MakeJWT(user.ID, cfg.secret, time.Hour)
	if err != nil {
		log.Printf("Error genrating access token: %v", err)
		res.WriteHeader(400)
		return
	}

	refresh_token, err := auth.MakeRefreshToken()
	if err != nil {
		log.Printf("Error generating refresh token: %v", err)
		res.WriteHeader(http.StatusInternalServerError)
		return
	}

	_, err = cfg.dbQueries.CreateToken(req.Context(), database.CreateTokenParams{
		Token:     refresh_token,
		UserID:    user.ID,
		ExpiresAt: time.Now().UTC().Add(time.Hour * 24 * 60),
	})
	if err != nil {
		log.Printf("Error saving refresh token: %v", err)
	}

	respBody := loginResponse{
		ID:           user.ID,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		AccessToken:  access_token,
		RefreshToken: refresh_token,
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

func validateChirp(body string) (string, error) {
	const maxChirpLength = 140
	if len(body) > maxChirpLength {
		return "", errors.New("Chirp is too long")
	}

	badWords := map[string]struct{}{
		"kerfuffle": {},
		"sharbert":  {},
		"fornax":    {},
	}
	cleaned := getCleanedBody(body, badWords)
	return cleaned, nil
}

func getCleanedBody(body string, badWords map[string]struct{}) string {
	words := strings.Split(body, " ")
	for i, word := range words {
		loweredWord := strings.ToLower(word)
		if _, ok := badWords[loweredWord]; ok {
			words[i] = "****"
		}
	}
	cleaned := strings.Join(words, " ")
	return cleaned
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

func respondWithError(req http.ResponseWriter, code int, msg string, err error) {
	if err != nil {
		log.Println(err)
	}
	if code > 499 {
		log.Printf("Responding with 5XX error: %s", msg)
	}
	type errorResponse struct {
		Error string `json:"error"`
	}
	respondWithJSON(req, code, errorResponse{
		Error: msg,
	})
}

func respondWithJSON(req http.ResponseWriter, code int, payload interface{}) {
	req.Header().Set("Content-Type", "application/json")
	dat, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		req.WriteHeader(500)
		return
	}
	req.WriteHeader(code)
	req.Write(dat)
}
