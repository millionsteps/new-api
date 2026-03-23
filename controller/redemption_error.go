package controller

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func handleRedemptionError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, model.ErrRedeemFailed) {
		common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
		return
	}
	if strings.HasPrefix(err.Error(), "redemption.") || err.Error() == i18n.MsgRedeemFailed {
		common.ApiErrorI18n(c, err.Error())
		return
	}
	common.ApiError(c, err)
}
