package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"strconv"

	"github.com/gorilla/mux"
	"github.com/stianba/auth-service/token"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const collection string = "electricians"

type electrician struct {
	ID           bson.ObjectId `json:"_id" bson:"_id,omitempty"`
	Name         string        `json:"name"`
	AddressLine1 string        `json:"addressLine1" bson:"addressLine1"`
	AddressLine2 string        `json:"addressLine2" bson:"addressLine2"`
	City         string        `json:"city"`
	County       string        `json:"county"`
	Zip          string        `json:"zip"`
	Phone        string        `json:"phone"`
	Location     geo           `json:"location"`
}

type geo struct {
	Type        string    `json:"-"`
	Coordinates []float64 `json:"coordinates"`
}

type searchParams struct {
	Skip          int
	Limit         int
	Text          string
	Hint          string
	Lon           float64
	Lat           float64
	LocationScope int
}

func ensureIndex(s *mgo.Session) {
	session := s.Copy()
	defer session.Close()

	c := session.DB(os.Getenv("DB_NAME")).C(collection)

	geoIndex := mgo.Index{
		Key: []string{"$2dsphere:location"},
	}

	err := c.EnsureIndex(geoIndex)

	if err != nil {
		panic(err)
	}

	textSearchIndex := mgo.Index{
		Key: []string{"$text:name", "$text:addressLine1", "$text:addressLine2", "$text:city", "$text:county"},
	}

	err = c.EnsureIndex(textSearchIndex)

	if err != nil {
		panic(err)
	}

	hintIndex := mgo.Index{
		Key: []string{"name"},
	}

	err = c.EnsureIndex(hintIndex)

	if err != nil {
		panic(err)
	}
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

func search(s *mgo.Session) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := s.Copy()
		defer session.Close()

		var electricians []electrician

		query := make(bson.M, 0)
		params := searchParams{Skip: 0, Limit: 10, LocationScope: 90000}
		queries := r.URL.Query()

		skipQuery, ok := queries["skip"]

		if ok {
			if len(skipQuery) > 0 {
				i, err := strconv.ParseInt(skipQuery[0], 10, 64)

				if err != nil {
					panic(err)
				}

				params.Skip = int(i)
			}
		}

		limitQuery, ok := queries["limit"]

		if ok {
			if len(skipQuery) > 0 {
				i, err := strconv.ParseInt(limitQuery[0], 10, 64)

				if err != nil {
					panic(err)
				}

				params.Limit = int(i)
			}
		}

		textQuery, ok := queries["text"]

		if ok {
			if len(textQuery) > 0 {
				params.Text = textQuery[0]
			}
		}

		hintQuery, ok := queries["hint"]

		if ok {
			if len(hintQuery) > 0 {
				params.Hint = hintQuery[0]
			}
		}

		lonQuery, ok := queries["lon"]

		if ok {
			if len(lonQuery) > 0 {
				params.Lon, _ = strconv.ParseFloat(lonQuery[0], 64)
			}
		}

		latQuery, ok := queries["lat"]

		if ok {
			if len(latQuery) > 0 {
				params.Lat, _ = strconv.ParseFloat(latQuery[0], 64)
			}
		}

		if params.Text != "" {
			query["$text"] = bson.M{"$search": params.Text}
		}

		if params.Hint != "" {
			query["name"] = bson.M{"$regex": bson.RegEx{Pattern: "^" + params.Hint, Options: "i"}}
		}

		if params.Lon > 0 {
			query["location"] = bson.M{
				"$near": bson.M{
					"$geometry": bson.M{
						"type":        "Point",
						"coordinates": []float64{params.Lon, params.Lat},
					},
					"$maxDistance": params.LocationScope,
				},
			}
		}

		c := session.DB(os.Getenv("DB_NAME")).C(collection)
		c.Find(query).Skip(params.Skip).Limit(params.Limit).Sort("name").All(&electricians)
		electriciansJSON, err := json.Marshal(electricians)

		if err != nil {
			log.Fatal(err)
		}

		responseWithJSON(w, electriciansJSON, http.StatusOK)
	}
}

func create(s *mgo.Session) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := s.Copy()
		defer session.Close()

		electrician := electrician{ID: bson.NewObjectId()}

		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&electrician)

		electrician.Location.Type = "Point"

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

func delete(s *mgo.Session) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		session := s.Copy()
		defer session.Close()

		vars := mux.Vars(r)
		id := vars["id"]

		c := session.DB(os.Getenv("DB_NAME")).C(collection)
		err := c.RemoveId(bson.ObjectIdHex(id))

		if err != nil {
			switch err {
			default:
				errorWithJSON(w, "Database error", http.StatusInternalServerError)
				return
			case mgo.ErrNotFound:
				errorWithJSON(w, "Electrician not found", http.StatusNotFound)
				return
			}
		}

		responseWithJSON(w, []byte(fmt.Sprint("{\"message\":\"electrician_deleted\"}")), http.StatusOK)
	}
}

func main() {
	session, err := mgo.Dial(fmt.Sprintf("mongodb://%v:%v@%v/%v", os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOST"), os.Getenv("DB_NAME")))

	if err != nil {
		panic(err)
	}

	defer session.Close()
	session.SetMode(mgo.Monotonic, true)
	ensureIndex(session)

	port := os.Getenv("PORT")

	if port == "" {
		port = "1338"
	}

	router := mux.NewRouter()
	router.HandleFunc("/", listAll(session)).Methods("GET")
	router.HandleFunc("/search", search(session)).Methods("GET")
	router.Handle("/", isAuthenticated(http.HandlerFunc(create(session)))).Methods("POST")
	router.Handle("/{id}", isAuthenticated(http.HandlerFunc(delete(session)))).Methods("DELETE")
	http.ListenAndServe(":"+port, router)
}
