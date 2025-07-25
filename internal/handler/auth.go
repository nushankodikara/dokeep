package handler

import (
	"database/sql"
	"dokeep/web/template"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB      *sql.DB
	Session *scs.SessionManager
}

func (h *AuthHandler) ShowRegistrationForm(w http.ResponseWriter, r *http.Request) {
	template.RegisterPage().Render(r.Context(), w)
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Error hashing password", http.StatusInternalServerError)
		return
	}

	var userID int
	err = h.DB.QueryRow("INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id", username, string(hashedPassword)).Scan(&userID)
	if err != nil {
		http.Error(w, "Error creating user", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *AuthHandler) ShowLoginForm(w http.ResponseWriter, r *http.Request) {
	template.LoginPage().Render(r.Context(), w)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	var storedPasswordHash string
	var userID int
	var totpEnabled bool
	err := h.DB.QueryRow("SELECT id, password_hash, totp_enabled FROM users WHERE username = $1", username).Scan(&userID, &storedPasswordHash, &totpEnabled)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(storedPasswordHash), []byte(password))
	if err != nil {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	// Store temporary user ID for TOTP verification
	h.Session.Put(r.Context(), "tempUserID", userID)

	if totpEnabled {
		http.Redirect(w, r, "/verify-totp", http.StatusSeeOther)
		return
	}

	if err := h.Session.RenewToken(r.Context()); err != nil {
		http.Error(w, "Failed to renew session token", http.StatusInternalServerError)
		return
	}

	h.Session.Remove(r.Context(), "tempUserID")
	h.Session.Put(r.Context(), "userID", userID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *AuthHandler) VerifyTOTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	totpCode := r.FormValue("totp_code")
	userID := h.Session.GetInt(r.Context(), "tempUserID")

	var secret string
	err := h.DB.QueryRow("SELECT totp_secret FROM users WHERE id = $1", userID).Scan(&secret)
	if err != nil {
		http.Error(w, "Failed to retrieve user data", http.StatusInternalServerError)
		return
	}

	valid := totp.Validate(totpCode, secret)
	if !valid {
		http.Error(w, "Invalid TOTP code", http.StatusBadRequest)
		return
	}

	if err := h.Session.RenewToken(r.Context()); err != nil {
		http.Error(w, "Failed to renew session token", http.StatusInternalServerError)
		return
	}

	h.Session.Remove(r.Context(), "tempUserID")
	h.Session.Put(r.Context(), "userID", userID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *AuthHandler) ShowSetupTOTPForm(w http.ResponseWriter, r *http.Request) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Dokeep",
		AccountName: "user@dokeep.com", // This should be dynamic based on the logged-in user
	})
	if err != nil {
		http.Error(w, "Failed to generate TOTP key", http.StatusInternalServerError)
		return
	}

	// Store the secret in the session for verification
	h.Session.Put(r.Context(), "totpSecret", key.Secret())

	var png []byte
	png, err = qrcode.Encode(key.URL(), qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	template.SetupTOTPPage(string(png)).Render(r.Context(), w)
}

func (h *AuthHandler) SetupTOTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	secret := h.Session.GetString(r.Context(), "totp_secret")
	if secret == "" {
		http.Error(w, "Could not find TOTP secret. Please try again.", http.StatusBadRequest)
		return
	}

	code := r.FormValue("totp_code")
	valid := totp.Validate(code, secret)
	if !valid {
		http.Error(w, "Invalid TOTP code. Please try again.", http.StatusBadRequest)
		return
	}

	userID := h.Session.GetInt(r.Context(), "userID")
	_, err := h.DB.Exec("UPDATE users SET totp_secret = $1, totp_enabled = TRUE WHERE id = $2", secret, userID)
	if err != nil {
		http.Error(w, "Failed to save TOTP secret", http.StatusInternalServerError)
		return
	}

	h.Session.Put(r.Context(), "totp_enabled", true)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *AuthHandler) ShowSettingsPage(w http.ResponseWriter, r *http.Request) {
	template.SettingsPage().Render(r.Context(), w)
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	userID := h.Session.GetInt(r.Context(), "userID")

	var storedPasswordHash string
	err := h.DB.QueryRow("SELECT password_hash FROM users WHERE id = $1", userID).Scan(&storedPasswordHash)
	if err != nil {
		http.Error(w, "Failed to retrieve user data", http.StatusInternalServerError)
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(storedPasswordHash), []byte(currentPassword))
	if err != nil {
		http.Error(w, "Incorrect current password", http.StatusUnauthorized)
		return
	}

	newHashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Error hashing new password", http.StatusInternalServerError)
		return
	}

	_, err = h.DB.Exec("UPDATE users SET password_hash = $1 WHERE id = $2", newHashedPassword, userID)
	if err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
