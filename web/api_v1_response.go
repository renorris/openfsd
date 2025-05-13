package main

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
)

type APIV1Response struct {
	Version string  `json:"version"`
	Err     *string `json:"err"`
	Data    any     `json:"data"`
}

const v1Version = "v1"

func newAPIV1Success(data any) APIV1Response {
	return APIV1Response{
		Version: v1Version,
		Err:     nil,
		Data:    data,
	}
}

func newAPIV1Failure(err string) APIV1Response {
	return APIV1Response{
		Version: v1Version,
		Err:     &err,
		Data:    nil,
	}
}

var genericAPIV1InternalServerError = newAPIV1Failure("internal server error")
var genericAPIV1Forbidden = newAPIV1Failure("forbidden")
var genericAPIV1NotFound = newAPIV1Failure("not found")

// bindJSONOrAbort attempts to JSON bind a given request body. On failure,
// it aborts the request with http.StatusBadRequest and returns ok = false.
func bindJSONOrAbort(c *gin.Context, reqBody any) (ok bool) {
	if err := c.ShouldBindJSON(reqBody); err != nil {
		res := newAPIV1Failure("invalid JSON body")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	return true
}

func writeAPIV1Response(c *gin.Context, code int, res *APIV1Response) {
	resBody, err := json.Marshal(res)
	if err != nil {
		c.Writer.Header().Set("Content-Type", "text/plain")
		c.Writer.WriteHeader(http.StatusInternalServerError)
		c.Writer.Write([]byte("internal server error\n\nbad response body"))
		return
	}

	c.Writer.Header().Add("Content-Type", "application/json")
	c.Writer.WriteHeader(code)
	c.Writer.Write(resBody)
}
