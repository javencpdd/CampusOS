package handler

import "github.com/gin-gonic/gin"

func currentUser(c *gin.Context) (string, string, bool) {
	userID, ok := c.Get("user_id")
	if !ok {
		return "", "", false
	}

	id, ok := userID.(string)
	if !ok || id == "" {
		return "", "", false
	}

	displayName := "Anonymous"
	if value, ok := c.Get("nickname"); ok {
		if name, ok := value.(string); ok && name != "" {
			displayName = name
		}
	}
	if displayName == "Anonymous" {
		if value, ok := c.Get("username"); ok {
			if name, ok := value.(string); ok && name != "" {
				displayName = name
			}
		}
	}

	return id, displayName, true
}
