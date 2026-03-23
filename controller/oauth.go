package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const pendingOAuthRegistrationSessionKey = "pending_oauth_registration"

type pendingOAuthRegistration struct {
	Provider       string `json:"provider"`
	ProviderUserID string `json:"provider_user_id"`
	Username       string `json:"username,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	Email          string `json:"email,omitempty"`
}

type OAuthRegisterRequest struct {
	RedemptionCode string `json:"redemption_code"`
}

// providerParams returns map with Provider key for i18n templates
func providerParams(name string) map[string]any {
	return map[string]any{"Provider": name}
}

// GenerateOAuthCode generates a state code for OAuth CSRF protection
func GenerateOAuthCode(c *gin.Context) {
	session := sessions.Default(c)
	state := common.GetRandomString(12)
	affCode := c.Query("aff")
	if affCode != "" {
		session.Set("aff", affCode)
	}
	session.Set("oauth_state", state)
	err := session.Save()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    state,
	})
}

// HandleOAuth handles OAuth callback for all standard OAuth providers
func HandleOAuth(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}

	session := sessions.Default(c)

	// 1. Validate state (CSRF protection)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthStateInvalid),
		})
		return
	}

	// 2. Check if user is already logged in (bind flow)
	username := session.Get("username")
	if username != nil {
		handleOAuthBind(c, provider)
		return
	}

	// 3. Check if provider is enabled
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// 4. Handle error from provider
	errorCode := c.Query("error")
	if errorCode != "" {
		errorDescription := c.Query("error_description")
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": errorDescription,
		})
		return
	}

	// 5. Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 6. Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// 7. Find or create user
	user, exists, err := findExistingOAuthUser(provider, oauthUser)
	if err != nil {
		switch err.(type) {
		case *OAuthUserDeletedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		case *OAuthRegistrationDisabledError:
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		default:
			common.ApiError(c, err)
		}
		return
	}
	if !exists {
		if !common.RegisterEnabled {
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
			return
		}
		if common.RegisterWithRedemptionCodeEnabled {
			if err := savePendingOAuthRegistration(session, providerName, oauthUser); err != nil {
				common.ApiError(c, err)
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "",
				"data": gin.H{
					"require_redemption_code": true,
					"register_path":           "/register/redemption",
					"provider":                providerName,
				},
			})
			return
		}

		user, err = createOAuthUser(provider, oauthUser, session)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	// 8. Check user status
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	// 9. Setup login
	setupLogin(user, c)
}

func GetPendingOAuthRegistration(c *gin.Context) {
	session := sessions.Default(c)
	pending, err := getPendingOAuthRegistration(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if pending == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"pending": false,
			},
		})
		return
	}

	providerName := pending.Provider
	if provider := oauth.GetProvider(pending.Provider); provider != nil {
		providerName = provider.GetName()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"pending":      true,
			"provider":     pending.Provider,
			"providerName": providerName,
			"display_name": pending.DisplayName,
			"email":        pending.Email,
		},
	})
}

func CompleteOAuthRegistration(c *gin.Context) {
	if !common.RegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		return
	}
	if !common.RegisterWithRedemptionCodeEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterRedemptionDisabled)
		return
	}

	session := sessions.Default(c)
	pending, err := getPendingOAuthRegistration(session)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if pending == nil {
		common.ApiErrorMsg(c, "oauth registration session expired")
		return
	}

	var request OAuthRegisterRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if request.RedemptionCode == "" {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterRedemptionRequired)
		return
	}

	provider := oauth.GetProvider(pending.Provider)
	if provider == nil {
		common.ApiErrorMsg(c, "oauth provider not found")
		return
	}

	if provider.IsUserIDTaken(pending.ProviderUserID) {
		user, _, findErr := findExistingOAuthUser(provider, &oauth.OAuthUser{
			ProviderUserID: pending.ProviderUserID,
		})
		if findErr != nil {
			common.ApiError(c, findErr)
			return
		}
		if err := clearPendingOAuthRegistration(session); err != nil {
			common.ApiError(c, err)
			return
		}
		setupLogin(user, c)
		return
	}

	oauthUser := &oauth.OAuthUser{
		ProviderUserID: pending.ProviderUserID,
		Username:       pending.Username,
		DisplayName:    pending.DisplayName,
		Email:          pending.Email,
	}

	user, err := createOAuthUserWithRedemption(provider, oauthUser, session, request.RedemptionCode)
	if err != nil {
		handleRedemptionError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}
	if err := clearPendingOAuthRegistration(session); err != nil {
		common.ApiError(c, err)
		return
	}

	setupLogin(user, c)
}

// handleOAuthBind handles binding OAuth account to existing user
func handleOAuthBind(c *gin.Context, provider oauth.Provider) {
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	// Exchange code for token
	code := c.Query("code")
	token, err := provider.ExchangeToken(c.Request.Context(), code, c)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Get user info
	oauthUser, err := provider.GetUserInfo(c.Request.Context(), token)
	if err != nil {
		handleOAuthError(c, err)
		return
	}

	// Check if this OAuth account is already bound (check both new ID and legacy ID)
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
		return
	}
	// Also check legacy ID to prevent duplicate bindings during migration period
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		if provider.IsUserIDTaken(legacyID) {
			common.ApiErrorI18n(c, i18n.MsgOAuthAlreadyBound, providerParams(provider.GetName()))
			return
		}
	}

	// Get current user from session
	session := sessions.Default(c)
	id := session.Get("id")
	user := model.User{Id: id.(int)}
	err = user.FillUserById()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Handle binding based on provider type
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: use user_oauth_bindings table
		err = model.UpdateUserOAuthBinding(user.Id, genericProvider.GetProviderId(), oauthUser.ProviderUserID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		// Built-in provider: update user record directly
		provider.SetProviderUserID(&user, oauthUser.ProviderUserID)
		err = user.Update(false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	common.ApiSuccessI18n(c, i18n.MsgOAuthBindSuccess, nil)
}

// findExistingOAuthUser finds an existing user by OAuth binding.
func findExistingOAuthUser(provider oauth.Provider, oauthUser *oauth.OAuthUser) (*model.User, bool, error) {
	user := &model.User{}

	// Check if user already exists with new ID
	if provider.IsUserIDTaken(oauthUser.ProviderUserID) {
		err := provider.FillUserByProviderID(user, oauthUser.ProviderUserID)
		if err != nil {
			return nil, false, err
		}
		// Check if user has been deleted
		if user.Id == 0 {
			return nil, false, &OAuthUserDeletedError{}
		}
		return user, true, nil
	}

	// Try to find user with legacy ID (for GitHub migration from login to numeric ID)
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok && legacyID != "" {
		if provider.IsUserIDTaken(legacyID) {
			err := provider.FillUserByProviderID(user, legacyID)
			if err != nil {
				return nil, false, err
			}
			if user.Id != 0 {
				// Found user with legacy ID, migrate to new ID
				common.SysLog(fmt.Sprintf("[OAuth] Migrating user %d from legacy_id=%s to new_id=%s",
					user.Id, legacyID, oauthUser.ProviderUserID))
				if err := user.UpdateGitHubId(oauthUser.ProviderUserID); err != nil {
					common.SysError(fmt.Sprintf("[OAuth] Failed to migrate user %d: %s", user.Id, err.Error()))
					// Continue with login even if migration fails
				}
				return user, true, nil
			}
		}
	}

	return nil, false, nil
}

func createOAuthUser(provider oauth.Provider, oauthUser *oauth.OAuthUser, session sessions.Session) (*model.User, error) {
	return createOAuthUserWithRedemption(provider, oauthUser, session, "")
}

func createOAuthUserWithRedemption(provider oauth.Provider, oauthUser *oauth.OAuthUser, session sessions.Session, redemptionCode string) (*model.User, error) {
	if !common.RegisterEnabled {
		return nil, &OAuthRegistrationDisabledError{}
	}

	user := &model.User{}

	// Set up new user
	user.Username = provider.GetProviderPrefix() + strconv.Itoa(model.GetMaxUserId()+1)

	if oauthUser.Username != "" {
		if exists, err := model.CheckUserExistOrDeleted(oauthUser.Username, ""); err == nil && !exists {
			// 防止索引退化
			if len(oauthUser.Username) <= model.UserNameMaxLength {
				user.Username = oauthUser.Username
			}
		}
	}

	if oauthUser.DisplayName != "" {
		user.DisplayName = oauthUser.DisplayName
	} else if oauthUser.Username != "" {
		user.DisplayName = oauthUser.Username
	} else {
		user.DisplayName = provider.GetName() + " User"
	}
	if oauthUser.Email != "" {
		user.Email = oauthUser.Email
	}
	user.Role = common.RoleCommonUser
	user.Status = common.UserStatusEnabled

	// Handle affiliate code
	affCode := session.Get("aff")
	inviterId := 0
	if affCode != nil {
		inviterId, _ = model.GetUserIdByAffCode(affCode.(string))
	}

	// Use transaction to ensure user creation and OAuth binding are atomic
	if genericProvider, ok := provider.(*oauth.GenericOAuthProvider); ok {
		// Custom provider: create user and binding in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, inviterId); err != nil {
				return err
			}

			// Create OAuth binding
			binding := &model.UserOAuthBinding{
				UserId:         user.Id,
				ProviderId:     genericProvider.GetProviderId(),
				ProviderUserId: oauthUser.ProviderUserID,
			}
			if err := model.CreateUserOAuthBindingWithTx(tx, binding); err != nil {
				return err
			}
			if redemptionCode != "" {
				if _, err := model.RedeemWithRegisterTx(tx, redemptionCode, user.Id); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		// Perform post-transaction tasks (logs, sidebar config, inviter rewards)
		user.FinalizeOAuthUserCreation(inviterId)
	} else {
		// Built-in provider: create user and update provider ID in a transaction
		err := model.DB.Transaction(func(tx *gorm.DB) error {
			// Create user
			if err := user.InsertWithTx(tx, inviterId); err != nil {
				return err
			}

			// Set the provider user ID on the user model and update
			provider.SetProviderUserID(user, oauthUser.ProviderUserID)
			if err := tx.Model(user).Updates(map[string]interface{}{
				"github_id":   user.GitHubId,
				"discord_id":  user.DiscordId,
				"oidc_id":     user.OidcId,
				"linux_do_id": user.LinuxDOId,
				"wechat_id":   user.WeChatId,
				"telegram_id": user.TelegramId,
			}).Error; err != nil {
				return err
			}
			if redemptionCode != "" {
				if _, err := model.RedeemWithRegisterTx(tx, redemptionCode, user.Id); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		// Perform post-transaction tasks
		user.FinalizeOAuthUserCreation(inviterId)
	}

	return user, nil
}

func savePendingOAuthRegistration(session sessions.Session, provider string, oauthUser *oauth.OAuthUser) error {
	payload := pendingOAuthRegistration{
		Provider:       provider,
		ProviderUserID: oauthUser.ProviderUserID,
		Username:       oauthUser.Username,
		DisplayName:    oauthUser.DisplayName,
		Email:          oauthUser.Email,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	session.Set(pendingOAuthRegistrationSessionKey, string(raw))
	return session.Save()
}

func getPendingOAuthRegistration(session sessions.Session) (*pendingOAuthRegistration, error) {
	raw := session.Get(pendingOAuthRegistrationSessionKey)
	if raw == nil {
		return nil, nil
	}
	rawString, ok := raw.(string)
	if !ok || rawString == "" {
		return nil, nil
	}
	var pending pendingOAuthRegistration
	if err := json.Unmarshal([]byte(rawString), &pending); err != nil {
		return nil, err
	}
	return &pending, nil
}

func clearPendingOAuthRegistration(session sessions.Session) error {
	session.Delete(pendingOAuthRegistrationSessionKey)
	return session.Save()
}

// Error types for OAuth
type OAuthUserDeletedError struct{}

func (e *OAuthUserDeletedError) Error() string {
	return "user has been deleted"
}

type OAuthRegistrationDisabledError struct{}

func (e *OAuthRegistrationDisabledError) Error() string {
	return "registration is disabled"
}

// handleOAuthError handles OAuth errors and returns translated message
func handleOAuthError(c *gin.Context, err error) {
	switch e := err.(type) {
	case *oauth.OAuthError:
		if e.Params != nil {
			common.ApiErrorI18n(c, e.MsgKey, e.Params)
		} else {
			common.ApiErrorI18n(c, e.MsgKey)
		}
	case *oauth.AccessDeniedError:
		common.ApiErrorMsg(c, e.Message)
	case *oauth.TrustLevelError:
		common.ApiErrorI18n(c, i18n.MsgOAuthTrustLevelLow)
	default:
		common.ApiError(c, err)
	}
}
