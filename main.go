package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const dbName string = "electricians-service"

type electrician struct {
	ID      bson.ObjectId `json:"id" bson:"_id,omitempty"`
	Name    string        `json:"name"`
	Address string        `json:"address"`
}

func errorWithJSON(w http.ResponseWriter, err string, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprintf(w, "{\"message\": %q}", err)
}

func responseWithJSON(w http.ResponseWriter, json []byte, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write(json)
}

func isAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string

		tokens, ok := r.Header["Authorization"]

		if ok && len(tokens) >= 1 {
			token = tokens[0]
			token = strings.TrimPrefix(token, "Bearer ")
		}

		if token == "" {
			errorWithJSON(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				msg := fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
				return nil, msg
			}

			return []byte(os.Getenv("JWT_SIGNER_SECRET")), nil
		})

		if err != nil {
			errorWithJSON(w, "Invalid token.", http.StatusUnauthorized)
			return
		}

		if parsedToken != nil && parsedToken.Valid {
			next.ServeHTTP(w, r)
		} else {
			errorWithJSON(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		}
	})
}

func listAll(s *mgo.Session) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := s.Copy()
		defer session.Close()

		var electricians []electrician

		c := session.DB(dbName).C("electricians")
		err := c.Find(bson.M{}).All(&electricians)

		if err != nil {
			errorWithJSON(w, "Database error", http.StatusInternalServerError)
			log.Println("Failed get all books: ", err)
			return
		}

		jsonData, err := json.Marshal(electricians)

		if err != nil {
			log.Fatal(err)
		}

		responseWithJSON(w, jsonData, http.StatusOK)
	}
}

func create(s *mgo.Session) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := s.Copy()
		defer session.Close()

		var electrician electrician

		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&electrician)

		if err != nil {
			errorWithJSON(w, "Icorrect body", http.StatusBadRequest)
			return
		}

		c := session.DB(dbName).C("electricians")
		err = c.Insert(electrician)

		if err != nil {
			errorWithJSON(w, "Database error", http.StatusInternalServerError)
			log.Println("Failed insert book: ", err)
			return
		}

		electricianJSON, _ := json.Marshal(electrician)
		responseWithJSON(w, electricianJSON, http.StatusCreated)
	}
}

func main() {
	session, err := mgo.Dial("localhost")

	if err != nil {
		panic(err)
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	router := mux.NewRouter()
	router.HandleFunc("/", listAll(session)).Methods("GET")
	router.Handle("/", isAuthenticated(http.HandlerFunc(create(session)))).Methods("POST")
	http.ListenAndServe(":1337", router)
}
