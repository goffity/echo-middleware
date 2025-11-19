package echomiddleware

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func BodyDump(c echo.Context, reqBody, resBody []byte) {
	if (viper.GetString("ENVIRONMENT") != "production") && c.Path() != "/healthz" {

		reqBodyString := string(reqBody)
		reqBodyString = strings.ReplaceAll(reqBodyString, "\n", "")
		reqBodyString = strings.ReplaceAll(reqBodyString, "\r", "")
		reqBodyString = strings.ReplaceAll(reqBodyString, "\t", "")

		resBodyString := string(resBody)
		resBodyString = strings.ReplaceAll(resBodyString, "\n", "")
		resBodyString = strings.ReplaceAll(resBodyString, "\r", "")
		resBodyString = strings.ReplaceAll(resBodyString, "\t", "")

		j, _ := json.Marshal(BodyDumpModel{
			Host:          c.Request().Host,
			Path:          c.Path(),
			Method:        c.Request().Method,
			RemoteAddress: c.Request().RemoteAddr,
			Header:        fmt.Sprintf("%v", c.Request().Header),
			Status:        c.Response().Status,
			Request:       reqBodyString,
			Response:      resBodyString,
		})

		zap.S().Infof("Body dump: %s", string(j))
	}
}
