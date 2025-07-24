package main

import (
	"bytes"
	"image/png"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dokeep/internal/database"
	"dokeep/internal/handler"
	"dokeep/internal/middleware"
	"dokeep/web/template"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/pquerna/otp/totp"
)

var sessionManager *scs.SessionManager

func main() {
	db := database.InitDB("data/dokeep.db")
	defer db.Close()

	// Initialize session manager
	sessionManager = scs.New()
	sessionManager.Store = sqlite3store.New(db)
	sessionManager.Lifetime = 12 * time.Hour

	authHandler := &handler.AuthHandler{DB: db, Session: sessionManager}
	docHandler := &handler.DocumentHandler{DB: db, Session: sessionManager}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if sessionManager.Exists(r.Context(), "userID") {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		template.IndexPage().Render(r.Context(), w)
	})

	mux.HandleFunc("/dashboard", middleware.RequireAuth(sessionManager, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		pageStr := r.URL.Query().Get("page")
		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}

		username := sessionManager.GetString(r.Context(), "username")
		docs, totalDocs, err := docHandler.List(w, r)
		if err != nil {
			http.Error(w, "Failed to list documents", http.StatusInternalServerError)
			return
		}

		totalPages := (totalDocs + 9) / 10
		template.DashboardPage(username, docs, totalDocs, page, totalPages, query).Render(r.Context(), w)
	}))

	mux.HandleFunc("/document", middleware.RequireAuth(sessionManager, docHandler.Show))

	mux.HandleFunc("/document/", middleware.RequireAuth(sessionManager, func(w http.ResponseWriter, r *http.Request) {
		// For POST requests, we need to parse the form to check for _method or other fields
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Failed to parse form", http.StatusBadRequest)
				return
			}
		}

		// Routing logic based on path segments
		trimmedPath := strings.TrimPrefix(r.URL.Path, "/document/")
		switch {
		case strings.HasSuffix(trimmedPath, "/tags"):
			docHandler.AddTag(w, r)
		case strings.Contains(trimmedPath, "/tags/"):
			if r.PostFormValue("_method") == "DELETE" {
				docHandler.RemoveTag(w, r)
			} else {
				http.NotFound(w, r)
			}
		case strings.HasSuffix(trimmedPath, "/details"):
			docHandler.UpdateDetails(w, r)
		case strings.HasSuffix(trimmedPath, "/date"):
			docHandler.UpdateDate(w, r)
		case r.PostFormValue("_method") == "DELETE":
			docHandler.Delete(w, r)
		default:
			http.NotFound(w, r)
		}
	}))

	mux.HandleFunc("/train", middleware.RequireAuth(sessionManager, docHandler.Train))

	mux.HandleFunc("/settings", middleware.RequireAuth(sessionManager, authHandler.ShowSettingsPage))
	mux.HandleFunc("/settings/password", middleware.RequireAuth(sessionManager, authHandler.ChangePassword))

	mux.HandleFunc("/uploads/", middleware.RequireAuth(sessionManager, func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))).ServeHTTP(w, r)
	}))

	mux.HandleFunc("/upload", middleware.RequireAuth(sessionManager, func(w http.ResponseWriter, r *http.Request) {
		docHandler.Upload(w, r)
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	}))

	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			authHandler.Register(w, r)
		} else {
			authHandler.ShowRegistrationForm(w, r)
		}
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			authHandler.Login(w, r)
		} else {
			authHandler.ShowLoginForm(w, r)
		}
	})

	mux.HandleFunc("/setup-totp", func(w http.ResponseWriter, r *http.Request) {
		userID := sessionManager.Get(r.Context(), "userID")
		var username string
		err := db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username)
		if err != nil {
			log.Printf("Error fetching username for TOTP setup: %v", err)
			http.Error(w, "Could not retrieve user information", http.StatusInternalServerError)
			return
		}

		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "Dokeep",
			AccountName: username,
		})
		if err != nil {
			log.Printf("Error generating TOTP key: %v", err)
			http.Error(w, "Could not generate TOTP key", http.StatusInternalServerError)
			return
		}

		// Store the secret in the session so we can verify it later
		sessionManager.Put(r.Context(), "totp_secret", key.Secret())

		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				log.Printf("Error parsing form for TOTP setup: %v", err)
				http.Error(w, "Could not parse form", http.StatusBadRequest)
				return
			}
			r.Form.Set("secret", key.Secret())
			authHandler.SetupTOTP(w, r)
			return
		}

		// Generate QR code
		var buf bytes.Buffer
		img, err := key.Image(200, 200)
		if err != nil {
			log.Printf("Error generating QR code image: %v", err)
			http.Error(w, "Could not generate QR code", http.StatusInternalServerError)
			return
		}
		png.Encode(&buf, img)

		template.SetupTOTPPage(buf.String()).Render(r.Context(), w)
	})

	mux.HandleFunc("/verify-totp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			authHandler.VerifyTOTP(w, r)
		} else {
			template.VerifyTOTPPage().Render(r.Context(), w)
		}
	})

	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		_ = sessionManager.Destroy(r.Context())
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	log.Println("Server starting on :8081")
	if err := http.ListenAndServe(":8081", sessionManager.LoadAndSave(mux)); err != nil {
		log.Fatalf("could not listen on port 8081 %v", err)
	}
}
