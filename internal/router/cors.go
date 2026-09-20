package router

import (
	"github.com/gin-contrib/cors"
	"time"
)

func routerCORSConfig() cors.Config {
	return cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			"Origin", "Content-Type", "Accept", "Authorization", "X-API-Key", "X-Request-ID", "X-Tenant-ID",
			"X-Embed-Session", "X-External-User-ID", "X-External-User-Token", "X-WeKnora-Desktop-Token",
			// Streamable HTTP MCP clients running in a browser send these on
			// the /mcp/:endpoint_id surface.
			"MCP-Protocol-Version", "Mcp-Session-Id", "Last-Event-ID",
		},
		ExposeHeaders:    []string{"Content-Length", "Access-Control-Allow-Origin", "Mcp-Session-Id"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
}
