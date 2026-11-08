package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

func (a *ClientController) domainTraffic(c *gin.Context) {
	from, e1 := strconv.ParseInt(c.Query("from"), 10, 64)
	to, e2 := strconv.ParseInt(c.Query("to"), 10, 64)
	client, e3 := strconv.Atoi(c.DefaultQuery("clientId", "0"))
	if e1 != nil || e2 != nil || e3 != nil || client < 0 || from < 0 || to <= from || to > 253402300799 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "Invalid client or time range"})
		return
	}
	location, err := a.settingService.GetTimeLocation()
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	svc := service.DomainTrafficService{}
	obj, err := svc.Query(service.DomainTrafficQuery{ClientId: client, From: from, To: to, Service: c.Query("service"), Domain: strings.ToLower(strings.TrimSpace(c.Query("domain"))), Search: strings.TrimSpace(c.Query("search"))}, location)
	jsonObj(c, obj, err)
}
