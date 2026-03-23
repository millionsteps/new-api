package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedRedemptionTestUser(t *testing.T) *User {
	t.Helper()

	user := &User{
		Username: fmt.Sprintf("redeem_user_%d", time.Now().UnixNano()),
		Password: "hashed-password",
		Status:   common.UserStatusEnabled,
		Role:     common.RoleCommonUser,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func seedRedemptionCode(t *testing.T, registerEnabled bool, registerOnly bool) *Redemption {
	t.Helper()

	redemption := &Redemption{
		UserId:          1,
		Key:             fmt.Sprintf("redeem_code_%d", time.Now().UnixNano()),
		Status:          common.RedemptionCodeStatusEnabled,
		Name:            "test redemption",
		Quota:           123,
		RegisterEnabled: registerEnabled,
		RegisterOnly:    registerOnly,
		CreatedTime:     common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return redemption
}

func TestRedeemRejectsRegisterOnlyCodeForTopUp(t *testing.T) {
	truncateTables(t)

	user := seedRedemptionTestUser(t)
	redemption := seedRedemptionCode(t, true, true)

	quota, err := Redeem(redemption.Key, user.Id)
	require.Error(t, err)
	require.Equal(t, 0, quota)
	require.Equal(t, i18n.MsgRedemptionRegisterOnly, err.Error())

	var refreshedUser User
	require.NoError(t, DB.First(&refreshedUser, user.Id).Error)
	require.Equal(t, 0, refreshedUser.Quota)

	var refreshedRedemption Redemption
	require.NoError(t, DB.First(&refreshedRedemption, redemption.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusEnabled, refreshedRedemption.Status)
	require.Equal(t, 0, refreshedRedemption.UsedUserId)
}

func TestRedeemWithRegisterTxAllowsRegisterOnlyCode(t *testing.T) {
	truncateTables(t)

	user := seedRedemptionTestUser(t)
	redemption := seedRedemptionCode(t, true, true)

	var quota int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		quota, txErr = RedeemWithRegisterTx(tx, redemption.Key, user.Id)
		return txErr
	})
	require.NoError(t, err)
	require.Equal(t, redemption.Quota, quota)

	var refreshedUser User
	require.NoError(t, DB.First(&refreshedUser, user.Id).Error)
	require.Equal(t, redemption.Quota, refreshedUser.Quota)

	var refreshedRedemption Redemption
	require.NoError(t, DB.First(&refreshedRedemption, redemption.Id).Error)
	require.Equal(t, common.RedemptionCodeStatusUsed, refreshedRedemption.Status)
	require.Equal(t, user.Id, refreshedRedemption.UsedUserId)
}
