package request

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// BindJSONStrict decodes one JSON object, rejects unknown fields and then runs
// the same struct validation used by Gin. Dynamic object endpoints should keep
// using their own schema-aware decoder instead.
func BindJSONStrict(c *gin.Context, target interface{}) error {
	return bindJSONStrict(c, target, false)
}

// BindJSONStrictOptional applies the strict decoder when a body is present and
// accepts an empty body for endpoints whose metadata is optional.
func BindJSONStrictOptional(c *gin.Context, target interface{}) error {
	return bindJSONStrict(c, target, true)
}

func bindJSONStrict(c *gin.Context, target interface{}, optional bool) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if optional && err == io.EOF {
			return nil
		}
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain exactly one JSON value")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return binding.Validator.ValidateStruct(target)
}
