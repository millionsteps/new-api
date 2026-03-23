package model

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
)

// ErrRedeemFailed is returned when redemption fails due to database error.
var ErrRedeemFailed = errors.New("redeem.failed")

type redeemScene int

const (
	redeemSceneTopUp redeemScene = iota
	redeemSceneRegister
)

type Redemption struct {
	Id              int            `json:"id"`
	UserId          int            `json:"user_id"`
	Key             string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status          int            `json:"status" gorm:"default:1"`
	Name            string         `json:"name" gorm:"index"`
	Quota           int            `json:"quota" gorm:"default:100"`
	RegisterEnabled bool           `json:"register_enabled" gorm:"default:false"`
	RegisterOnly    bool           `json:"register_only" gorm:"default:false"`
	CreatedTime     int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime    int64          `json:"redeemed_time" gorm:"bigint"`
	Count           int            `json:"count" gorm:"-:all"`
	UsedUserId      int            `json:"used_user_id"`
	DeletedAt       gorm.DeletedAt `gorm:"index"`
	ExpiredTime     int64          `json:"expired_time" gorm:"bigint"`
}

func GetAllRedemptions(startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	err = tx.Model(&Redemption{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func SearchRedemptions(keyword string, startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&Redemption{})
	if id, convErr := strconv.Atoi(keyword); convErr == nil {
		query = query.Where("id = ? OR name LIKE ?", id, keyword+"%")
	} else {
		query = query.Where("name LIKE ?", keyword+"%")
	}

	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func GetRedemptionById(id int) (*Redemption, error) {
	if id == 0 {
		return nil, errors.New("id 为空")
	}
	redemption := Redemption{Id: id}
	err := DB.First(&redemption, "id = ?", id).Error
	return &redemption, err
}

func redeemWithTx(tx *gorm.DB, key string, userId int, scene redeemScene) (*Redemption, error) {
	if key == "" {
		return nil, errors.New(i18n.MsgRedemptionNotProvided)
	}
	if userId == 0 {
		return nil, errors.New("invalid user id")
	}

	redemption := &Redemption{}
	keyCol := "`key`"
	if common.UsingPostgreSQL {
		keyCol = `"key"`
	}

	common.RandomSleep()
	err := tx.Set("gorm:query_option", "FOR UPDATE").Where(keyCol+" = ?", key).First(redemption).Error
	if err != nil {
		return nil, errors.New(i18n.MsgRedemptionInvalid)
	}
	if redemption.Status != common.RedemptionCodeStatusEnabled {
		return nil, errors.New(i18n.MsgRedemptionUsed)
	}
	if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
		return nil, errors.New(i18n.MsgRedemptionExpired)
	}

	if scene == redeemSceneRegister && !redemption.RegisterEnabled {
		return nil, errors.New(i18n.MsgRedemptionRegisterDisabled)
	}
	if scene == redeemSceneTopUp && redemption.RegisterOnly {
		return nil, errors.New(i18n.MsgRedemptionRegisterOnly)
	}

	err = tx.Model(&User{}).Where("id = ?", userId).Update("quota", gorm.Expr("quota + ?", redemption.Quota)).Error
	if err != nil {
		return nil, err
	}

	redemption.RedeemedTime = common.GetTimestamp()
	redemption.Status = common.RedemptionCodeStatusUsed
	redemption.UsedUserId = userId
	if err = tx.Save(redemption).Error; err != nil {
		return nil, err
	}

	return redemption, nil
}

func Redeem(key string, userId int) (quota int, err error) {
	var redemption *Redemption
	err = DB.Transaction(func(tx *gorm.DB) error {
		var txErr error
		redemption, txErr = redeemWithTx(tx, key, userId, redeemSceneTopUp)
		return txErr
	})
	if err != nil {
		common.SysError("redemption failed: " + err.Error())
		if isRedemptionBusinessError(err) {
			return 0, err
		}
		return 0, ErrRedeemFailed
	}

	RecordLog(userId, LogTypeTopup, fmt.Sprintf("通过兑换码充值 %s，兑换码ID %d", logger.LogQuota(redemption.Quota), redemption.Id))
	return redemption.Quota, nil
}

func RedeemWithTx(tx *gorm.DB, key string, userId int) (quota int, err error) {
	redemption, err := redeemWithTx(tx, key, userId, redeemSceneTopUp)
	if err != nil {
		return 0, err
	}
	return redemption.Quota, nil
}

func RedeemWithRegisterTx(tx *gorm.DB, key string, userId int) (quota int, err error) {
	redemption, err := redeemWithTx(tx, key, userId, redeemSceneRegister)
	if err != nil {
		return 0, err
	}
	return redemption.Quota, nil
}

func (redemption *Redemption) Insert() error {
	return DB.Create(redemption).Error
}

func (redemption *Redemption) SelectUpdate() error {
	return DB.Model(redemption).Select("redeemed_time", "status").Updates(redemption).Error
}

func (redemption *Redemption) Update() error {
	return DB.Model(redemption).Select("name", "status", "quota", "register_enabled", "register_only", "redeemed_time", "expired_time").Updates(redemption).Error
}

func (redemption *Redemption) Delete() error {
	return DB.Delete(redemption).Error
}

func DeleteRedemptionById(id int) error {
	if id == 0 {
		return errors.New("id 为空")
	}
	redemption := Redemption{Id: id}
	err := DB.Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete()
}

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := DB.Where(
		"status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)",
		[]int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled},
		common.RedemptionCodeStatusEnabled,
		now,
	).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}

func isRedemptionBusinessError(err error) bool {
	switch err.Error() {
	case i18n.MsgRedemptionInvalid,
		i18n.MsgRedemptionUsed,
		i18n.MsgRedemptionExpired,
		i18n.MsgRedemptionNotProvided,
		i18n.MsgRedemptionRegisterOnly,
		i18n.MsgRedemptionRegisterDisabled:
		return true
	default:
		return false
	}
}
