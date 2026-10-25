package handler

import (
	"net/http"
	"net/netip"
	"time"

	"github.com/gin-gonic/gin"
	"snippetbook/internal/geo"
	"snippetbook/internal/model"
)

type mapAddress struct {
	IP       string               `json:"ip"`
	Location *model.IPGeolocation `json:"location,omitempty"`
	Status   string               `json:"status"`
	Error    string               `json:"error,omitempty"`
	Stale    bool                 `json:"stale"`
}

type mapService struct {
	ID        int64                  `json:"id"`
	Name      string                 `json:"name"`
	Hostname  string                 `json:"hostname"`
	Enabled   bool                   `json:"enabled"`
	LastCheck *model.Check           `json:"last_check,omitempty"`
	DNS       *model.ServiceLocation `json:"dns,omitempty"`
	Addresses []mapAddress           `json:"addresses"`
	Error     string                 `json:"error,omitempty"`
}

// Map reads only persisted data; viewing the map never triggers external I/O.
func (h *ServicesHandler) Map(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	services, err := h.filteredServices(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	bindings, err := h.Store.ServiceLocations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cache, err := h.Store.IPGeolocations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := make([]mapService, 0, len(services))
	for _, service := range services {
		item := mapService{ID: service.ID, Name: service.Name, Hostname: geo.Hostname(service.URL), Enabled: service.Enabled, LastCheck: service.LastCheck, Addresses: []mapAddress{}}
		binding, ok := bindings[service.ID]
		if !ok || binding.Hostname != item.Hostname {
			item.Error = "Определение IP и расположения ожидает фонового обновления"
		} else {
			item.DNS = &binding
			item.Error = binding.Error
			for _, ip := range binding.IPs {
				address := mapAddress{IP: ip, Status: "pending", Error: "Геолокация IP ожидает фонового обновления"}
				parsed, err := netip.ParseAddr(ip)
				if err != nil || !geo.PublicIP(parsed) {
					address.Status, address.Error = "special", "Локальный или специальный IP: внешняя геолокация не выполняется"
				} else if location, exists := cache[ip]; exists {
					address.Location = &location
					address.Status, address.Error = location.Status, location.Error
					address.Stale = geo.Stale(location, time.Now()) || binding.Error != "" || !time.Now().Before(binding.NextAttemptAt)
				}
				item.Addresses = append(item.Addresses, address)
			}
		}
		result = append(result, item)
	}
	c.JSON(http.StatusOK, gin.H{"services": result, "provider": "ip-api.com", "refresh_interval_hours": 24})
}
