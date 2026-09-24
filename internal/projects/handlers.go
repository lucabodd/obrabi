package projects

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lucabodd/obrabi/internal/platform/httpx"
)

// Handlers exposes the projects HTTP API. Every route needs the internal
// token and the user identity forwarded by the gateway.
type Handlers struct {
	Store *Store
	Log   *slog.Logger
	Loc   *time.Location
}

// Register mounts the routes on r.
func (h *Handlers) Register(r gin.IRouter) {
	g := r.Group("", httpx.RequireUser())

	g.GET("/towns", h.listTowns)
	g.POST("/towns", h.createTown)
	g.PUT("/towns/:id", h.renameTown)
	g.DELETE("/towns/:id", h.deleteTown)

	g.GET("/categories", h.listCategories)
	g.POST("/categories", h.createCategory)
	g.PUT("/categories/:id", h.renameCategory)
	g.DELETE("/categories/:id", h.deleteCategory)

	g.GET("/projects", h.listProjects)
	g.POST("/projects", h.createProject)
	g.GET("/projects/:id", h.getProject)
	g.PUT("/projects/:id", h.updateProject)
	g.DELETE("/projects/:id", h.deleteProject)
	g.POST("/projects/:id/finish", h.setStatus(StatusFinished))
	g.POST("/projects/:id/reopen", h.setStatus(StatusActive))
	g.GET("/projects/:id/pdf", h.pdf)

	g.POST("/projects/:id/items", h.createItem)
	g.PUT("/projects/:id/items/:itemId", h.updateItem)
	g.DELETE("/projects/:id/items/:itemId", h.deleteItem)

	g.POST("/projects/:id/payments", h.createPayment)
	g.PUT("/projects/:id/payments/:paymentId", h.updatePayment)
	g.DELETE("/projects/:id/payments/:paymentId", h.deletePayment)
}

func (h *Handlers) today() string { return time.Now().In(h.Loc).Format("2006-01-02") }

// fail maps store errors to HTTP responses.
func fail(c *gin.Context, err error, notFound, duplicate string) {
	if v, ok := IsValidation(err); ok {
		httpx.BadRequest(c, v.Message)
		return
	}
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.NotFound(c, notFound)
	case errors.Is(err, ErrDuplicate):
		httpx.Conflict(c, "duplicate", duplicate)
	default:
		httpx.Internal(c, err)
	}
}

const (
	msgTownNotFound              = "No s'ha trobat el poble."
	msgCategoryNotFound          = "No s'ha trobat la categoria."
	msgProjectNotFound           = "No s'ha trobat l'obra."
	msgItemNotFound              = "No s'ha trobat la partida."
	msgProjectOrCategoryNotFound = "No s'ha trobat l'obra o la categoria."
	msgPaymentNotFound           = "No s'ha trobat el cobrament."
	msgTownDuplicate             = "Ja tens un poble amb este nom."
	msgCategoryDup               = "Ja tens una categoria amb este nom."
)

type nameRequest struct {
	Name string `json:"name"`
}

// ---------------------------------------------------------------- towns

func (h *Handlers) listTowns(c *gin.Context) {
	towns, err := h.Store.ListTowns(c, httpx.UserID(c))
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"towns": towns})
}

func (h *Handlers) createTown(c *gin.Context) {
	var req nameRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	name, err := NormalizeName(req.Name, 80, "del poble")
	if err != nil {
		fail(c, err, "", "")
		return
	}
	town, err := h.Store.CreateTown(c, httpx.UserID(c), name)
	if err != nil {
		fail(c, err, msgTownNotFound, msgTownDuplicate)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"town": town})
}

func (h *Handlers) renameTown(c *gin.Context) {
	id, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	var req nameRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	name, err := NormalizeName(req.Name, 80, "del poble")
	if err != nil {
		fail(c, err, "", "")
		return
	}
	town, err := h.Store.RenameTown(c, httpx.UserID(c), id, name)
	if err != nil {
		fail(c, err, msgTownNotFound, msgTownDuplicate)
		return
	}
	c.JSON(http.StatusOK, gin.H{"town": town})
}

func (h *Handlers) deleteTown(c *gin.Context) {
	id, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	err := h.Store.DeleteTown(c, httpx.UserID(c), id)
	if errors.Is(err, ErrInUse) {
		httpx.Conflict(c, "in_use", "No es pot eliminar: encara hi ha obres en este poble.")
		return
	}
	if err != nil {
		fail(c, err, msgTownNotFound, "")
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------------------------------------------------------------- categories

func (h *Handlers) listCategories(c *gin.Context) {
	cats, err := h.Store.ListCategories(c, httpx.UserID(c))
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"categories": cats})
}

func (h *Handlers) createCategory(c *gin.Context) {
	var req nameRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	name, err := NormalizeName(req.Name, 60, "de la categoria")
	if err != nil {
		fail(c, err, "", "")
		return
	}
	cat, err := h.Store.CreateCategory(c, httpx.UserID(c), name)
	if err != nil {
		fail(c, err, msgCategoryNotFound, msgCategoryDup)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"category": cat})
}

func (h *Handlers) renameCategory(c *gin.Context) {
	id, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	var req nameRequest
	if !httpx.BindJSON(c, &req) {
		return
	}
	name, err := NormalizeName(req.Name, 60, "de la categoria")
	if err != nil {
		fail(c, err, "", "")
		return
	}
	cat, err := h.Store.RenameCategory(c, httpx.UserID(c), id, name)
	if err != nil {
		fail(c, err, msgCategoryNotFound, msgCategoryDup)
		return
	}
	c.JSON(http.StatusOK, gin.H{"category": cat})
}

func (h *Handlers) deleteCategory(c *gin.Context) {
	id, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	var mergeInto int64
	if v := c.Query("merge_into"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 || n == id {
			httpx.BadRequest(c, "Tria una altra categoria per a fusionar-les.")
			return
		}
		mergeInto = n
	}
	used, err := h.Store.DeleteCategory(c, httpx.UserID(c), id, mergeInto)
	if errors.Is(err, ErrInUse) {
		httpx.Conflict(c, "in_use", fmt.Sprintf(
			"No es pot eliminar: s'utilitza en %d %s. Fusiona-la amb una altra categoria.", used, plural(used, "partida", "partides")))
		return
	}
	if err != nil {
		fail(c, err, msgCategoryNotFound, "")
		return
	}
	c.Status(http.StatusNoContent)
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// ---------------------------------------------------------------- projects

func (h *Handlers) listProjects(c *gin.Context) {
	status := c.DefaultQuery("status", StatusActive)
	switch status {
	case StatusActive, StatusFinished:
	case "all":
		status = ""
	default:
		httpx.BadRequest(c, "Estat desconegut.")
		return
	}
	list, err := h.Store.ListProjects(c, httpx.UserID(c), status)
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"projects": list})
}

func (h *Handlers) getProject(c *gin.Context) {
	id, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	d, err := h.Store.GetProject(c, httpx.UserID(c), id)
	if err != nil {
		fail(c, err, msgProjectNotFound, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": d})
}

func (h *Handlers) createProject(c *gin.Context) {
	var in ProjectInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := in.normalize(h.today()); err != nil {
		fail(c, err, "", "")
		return
	}
	d, err := h.Store.CreateProject(c, httpx.UserID(c), in)
	if err != nil {
		fail(c, err, msgTownNotFound, "")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"project": d})
}

func (h *Handlers) updateProject(c *gin.Context) {
	id, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	var in ProjectInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := in.normalize(h.today()); err != nil {
		fail(c, err, "", "")
		return
	}
	d, err := h.Store.UpdateProject(c, httpx.UserID(c), id, in)
	if err != nil {
		fail(c, err, msgProjectNotFound, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": d})
}

func (h *Handlers) deleteProject(c *gin.Context) {
	id, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	if err := h.Store.DeleteProject(c, httpx.UserID(c), id); err != nil {
		fail(c, err, msgProjectNotFound, "")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) setStatus(status string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := httpx.ParamID(c, "id")
		if !ok {
			return
		}
		d, err := h.Store.SetStatus(c, httpx.UserID(c), id, status)
		if err != nil {
			fail(c, err, msgProjectNotFound, "")
			return
		}
		c.JSON(http.StatusOK, gin.H{"project": d})
	}
}

func (h *Handlers) pdf(c *gin.Context) {
	id, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	variant := PDFInternal
	if c.Query("variant") == string(PDFClient) {
		variant = PDFClient
	}
	d, err := h.Store.GetProject(c, httpx.UserID(c), id)
	if err != nil {
		fail(c, err, msgProjectNotFound, "")
		return
	}
	doc, err := RenderPDF(d, variant, time.Now().In(h.Loc))
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	disposition := "inline"
	if c.Query("download") == "1" {
		disposition = "attachment"
	}
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, PDFFilename(d, variant)))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/pdf", doc)
}

// ---------------------------------------------------------------- items

func (h *Handlers) createItem(c *gin.Context) {
	pid, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	var in ItemInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := in.normalize(h.today()); err != nil {
		fail(c, err, "", "")
		return
	}
	item, d, err := h.Store.CreateItem(c, httpx.UserID(c), pid, in)
	if err != nil {
		fail(c, err, msgProjectOrCategoryNotFound, "")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"item": item, "project": d})
}

func (h *Handlers) updateItem(c *gin.Context) {
	pid, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	iid, ok := httpx.ParamID(c, "itemId")
	if !ok {
		return
	}
	var in ItemInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := in.normalize(h.today()); err != nil {
		fail(c, err, "", "")
		return
	}
	item, d, err := h.Store.UpdateItem(c, httpx.UserID(c), pid, iid, in)
	if err != nil {
		fail(c, err, msgItemNotFound, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"item": item, "project": d})
}

func (h *Handlers) deleteItem(c *gin.Context) {
	pid, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	iid, ok := httpx.ParamID(c, "itemId")
	if !ok {
		return
	}
	d, err := h.Store.DeleteItem(c, httpx.UserID(c), pid, iid)
	if err != nil {
		fail(c, err, msgItemNotFound, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": d})
}

// ---------------------------------------------------------------- payments

func (h *Handlers) createPayment(c *gin.Context) {
	pid, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	var in PaymentInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := in.normalize(h.today()); err != nil {
		fail(c, err, "", "")
		return
	}
	pay, d, err := h.Store.CreatePayment(c, httpx.UserID(c), pid, in)
	if err != nil {
		fail(c, err, msgProjectNotFound, "")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"payment": pay, "project": d})
}

func (h *Handlers) updatePayment(c *gin.Context) {
	pid, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	payID, ok := httpx.ParamID(c, "paymentId")
	if !ok {
		return
	}
	var in PaymentInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := in.normalize(h.today()); err != nil {
		fail(c, err, "", "")
		return
	}
	pay, d, err := h.Store.UpdatePayment(c, httpx.UserID(c), pid, payID, in)
	if err != nil {
		fail(c, err, msgPaymentNotFound, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"payment": pay, "project": d})
}

func (h *Handlers) deletePayment(c *gin.Context) {
	pid, ok := httpx.ParamID(c, "id")
	if !ok {
		return
	}
	payID, ok := httpx.ParamID(c, "paymentId")
	if !ok {
		return
	}
	d, err := h.Store.DeletePayment(c, httpx.UserID(c), pid, payID)
	if err != nil {
		fail(c, err, msgPaymentNotFound, "")
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": d})
}
