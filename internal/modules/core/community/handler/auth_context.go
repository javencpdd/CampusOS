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

	username := "Anonymous"
	if value, ok := c.Get("username"); ok {
		if name, ok := value.(string); ok && name != "" {
			username = name
		}
	}

	return id, username, true
}
