package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/stianba/auth-service/token"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const collection string = "electricians"

type electrician struct {
	ID      bson.ObjectId `json:"_id" bson:"_id,omitempty"`
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
		authHeader, ok := r.Header["Authorization"]

		if ok {
			persistentData, err := token.FromHeader(authHeader)

			if err != nil {
				errorWithJSON(w, err.Error(), http.StatusBadRequest)
				return
			}

			ctx := token.ToContext(persistentData, r)
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			errorWithJSON(w, "No auth header found", http.StatusBadRequest)
		}
	})
}

func listAll(s *mgo.Session) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := s.Copy()
		defer session.Close()

		var electricians []electrician

		c := session.DB(os.Getenv("DB_NAME")).C(collection)
		err := c.Find(bson.M{}).All(&electricians)

		if err != nil {
			errorWithJSON(w, "Database error", http.StatusInternalServerError)
			log.Println("Failed get all electricians: ", err)
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

		c := session.DB(os.Getenv("DB_NAME")).C(collection)
		err = c.Insert(electrician)

		if err != nil {
			errorWithJSON(w, "Database error", http.StatusInternalServerError)
			log.Println("Failed insert electrician: ", err)
			return
		}

		electricianJSON, _ := json.Marshal(electrician)
		responseWithJSON(w, electricianJSON, http.StatusCreated)
	}
}

func main() {
	session, err := mgo.Dial(fmt.Sprintf("mongodb://%v:%v@%v/%v", os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOST"), os.Getenv("DB_NAME")))

	if err != nil {
		panic(err)
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)

	port := os.Getenv("PORT")

	if port == "" {
		port = "1338"
	}

	router := mux.NewRouter()
	router.HandleFunc("/", listAll(session)).Methods("GET")
	router.Handle("/", isAuthenticated(http.HandlerFunc(create(session)))).Methods("POST")
	http.ListenAndServe(":"+port, router)
}
