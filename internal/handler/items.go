package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"snippetbook/internal/crypto"
	"snippetbook/internal/model"
	"snippetbook/internal/store"
)

type ItemsHandler struct {
	Store *store.DB
	Vault *crypto.Vault
}

func NewItemsHandler(s *store.DB, v *crypto.Vault) *ItemsHandler {
	return &ItemsHandler{Store: s, Vault: v}
}

type itemRequest struct {
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Language string   `json:"language"`
	Content  string   `json:"content"`
	Tags     []string `json:"tags"`
	Favorite bool     `json:"favorite"`
	Secret   string   `json:"secret"`
}

func (h *ItemsHandler) List(c *gin.Context) {
	kind := c.Query("kind")
	tag := c.Query("tag")
	q := c.Query("q")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	items, err := h.Store.List(kind, tag, q, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for index := range items {
		renderItemContent(&items[index])
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *ItemsHandler) Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	it, err := h.Store.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// never leak secret value
	renderItemContent(&it)
	c.JSON(http.StatusOK, it)
}

func (h *ItemsHandler) Create(c *gin.Context) {
	var req itemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Title == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title is required"})
		return
	}
	if req.Kind == "" {
		req.Kind = model.KindNote
	}
	it := model.Item{
		Kind:     req.Kind,
		Title:    req.Title,
		Language: req.Language,
		Content:  req.Content,
		Tags:     req.Tags,
		Favorite: req.Favorite,
	}
	id, err := h.Store.Create(&it)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.Secret != "" {
		ct, nonce, err := h.Vault.EncryptSecret(req.Secret)
		if err != nil {
			_ = h.Store.Delete(id)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "secret encryption failed: " + err.Error()})
			return
		}
		if err := h.Store.SetSecret(id, ct, nonce); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	renderItemContent(&it)
	c.JSON(http.StatusCreated, it)
}

func (h *ItemsHandler) Update(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	var req itemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	it, err := h.Store.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if req.Title != "" {
		it.Title = req.Title
	}
	if req.Kind != "" {
		it.Kind = req.Kind
	}
	it.Language = req.Language
	it.Content = req.Content
	it.Tags = req.Tags
	it.Favorite = req.Favorite
	if err := h.Store.Update(&it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.Secret != "" {
		ct, nonce, err := h.Vault.EncryptSecret(req.Secret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "secret encryption failed: " + err.Error()})
			return
		}
		if err := h.Store.SetSecret(id, ct, nonce); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	renderItemContent(&it)
	c.JSON(http.StatusOK, it)
}

func renderItemContent(item *model.Item) {
	if item.Kind != model.KindSecret && item.Content != "" {
		item.RenderedHTML = renderMarkdown(item.Content)
	}
}

func (h *ItemsHandler) Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	if err := h.Store.SoftDelete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ItemsHandler) Reveal(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		return
	}
	ct, nonce, err := h.Store.GetSecret(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no secret stored"})
		return
	}
	val, err := h.Vault.DecryptSecret(ct, nonce)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "secret decryption failed"})
		return
	}
	c.JSON(http.StatusOK, model.SecretPayload{ItemID: id, Value: val})
}

func (h *ItemsHandler) Tags(c *gin.Context) {
	items, err := h.Store.List("", "", "", 10000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	seen := map[string]bool{}
	out := []string{}
	for _, it := range items {
		for _, t := range it.Tags {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"tags": out})
}

func parseID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return 0, err
	}
	return id, nil
}
