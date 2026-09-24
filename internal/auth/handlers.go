package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/lucabodd/obrabi/internal/platform/httpx"
)

// Handlers exposes the auth HTTP API. Every route requires the internal token;
// the /users routes also require the user identity set by the gateway.
type Handlers struct {
	Store *Store
	Log   *slog.Logger
}

// Register mounts the routes on r.
func (h *Handlers) Register(r gin.IRouter) {
	r.POST("/internal/login", h.login)

	me := r.Group("/users/me", httpx.RequireUser())
	me.GET("", h.me)
	me.PUT("", h.updateMe)
	me.PUT("/password", h.changePassword)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) login(c *gin.Context) {
	var req loginRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	username, err := NormalizeUsername(req.Username)
	if err != nil {
		// Still spend a bcrypt comparison: invalid names must not answer faster.
		CheckPassword("", req.Password)
		invalidCredentials(c)
		return
	}
	user, hash, err := h.Store.Credentials(c, username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		httpx.Internal(c, err)
		return
	}
	if !CheckPassword(hash, req.Password) {
		h.Log.Warn("failed login", "username", username)
		invalidCredentials(c)
		return
	}
	if err := h.Store.TouchLogin(c, user.ID); err != nil {
		h.Log.Warn("cannot record login", "err", err)
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func invalidCredentials(c *gin.Context) {
	httpx.Fail(c, http.StatusUnauthorized, "invalid_credentials", "L'usuari o la contrasenya no són correctes.")
}

func (h *Handlers) me(c *gin.Context) {
	user, err := h.Store.ByID(c, httpx.UserID(c))
	if errors.Is(err, ErrNotFound) {
		httpx.Fail(c, http.StatusUnauthorized, "unauthorized", "Cal iniciar la sessió.")
		return
	}
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

type updateMeRequest struct {
	DisplayName string `json:"display_name"`
}

func (h *Handlers) updateMe(c *gin.Context) {
	var req updateMeRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" || utf8.RuneCountInString(name) > 60 {
		httpx.BadRequest(c, "El nom ha de tindre entre 1 i 60 caràcters.")
		return
	}
	user, err := h.Store.SetDisplayName(c, httpx.UserID(c), name)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *Handlers) changePassword(c *gin.Context) {
	var req changePasswordRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	uid := httpx.UserID(c)
	hash, err := h.Store.PasswordHash(c, uid)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	if !CheckPassword(hash, req.CurrentPassword) {
		httpx.Fail(c, http.StatusForbidden, "wrong_password", "La contrasenya actual no és correcta.")
		return
	}
	newHash, err := HashPassword(req.NewPassword)
	if errors.Is(err, ErrWeakPassword) {
		httpx.BadRequest(c, "La contrasenya nova ha de tindre entre 8 i 72 caràcters.")
		return
	}
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	if err := h.Store.SetPassword(c, uid, newHash); err != nil {
		httpx.Internal(c, err)
		return
	}
	h.Log.Info("password changed", "user_id", uid)
	c.Status(http.StatusNoContent)
}
