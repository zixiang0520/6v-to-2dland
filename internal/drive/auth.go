package drive

import (
	"context"
	"time"

	"github.com/halalcloud/golang-sdk-lite/halalcloud/services/oauth"
)

// LoginResult 是发起设备码登录后返回给前端的信息。
type LoginResult struct {
	VerificationURI string `json:"verification_uri"`
	UserCode        string `json:"user_code"`
	ExpiresIn       int32  `json:"expires_in"`
	Interval        int32  `json:"interval"`
}

// PollResult 是轮询登录状态的结果。
type PollResult struct {
	Status   string `json:"status"` // AUTHORIZATION_SUCCESS / DEVICE_CODE / NO_LOGIN 等
	LoggedIn bool   `json:"logged_in"`
}

// StartLogin 发起设备码授权流程，返回用户需在浏览器中访问的地址和 user_code。
func (c *Client) StartLogin(ctx context.Context) (*LoginResult, error) {
	resp, err := c.oauth.DeviceCodeAuthorize(ctx, &oauth.AuthorizeRequest{
		ClientId: c.clientID,
		Device:   "6v-to-2dland/1.0",
	})
	if err != nil {
		return nil, err
	}
	c.deviceCode = resp.DeviceCode
	c.loginExpire = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	return &LoginResult{
		VerificationURI: resp.VerificationUri,
		UserCode:        resp.UserCode,
		ExpiresIn:       resp.ExpiresIn,
		Interval:        resp.Interval,
	}, nil
}

// PollLogin 轮询登录状态；授权成功时落盘 token。
func (c *Client) PollLogin(ctx context.Context) (*PollResult, error) {
	if c.deviceCode == "" || time.Now().After(c.loginExpire) {
		return &PollResult{Status: "NO_LOGIN"}, nil
	}
	state, err := c.oauth.GetDeviceCodeState(ctx, &oauth.DeviceCodeAuthorizeState{DeviceCode: c.deviceCode})
	if err != nil {
		return nil, err
	}
	res := &PollResult{Status: state.Status, LoggedIn: state.Login}
	if state.Status == "AUTHORIZATION_SUCCESS" && state.AccessToken != "" {
		c.api.SetToken(state.AccessToken, state.RefreshToken, state.ExpiresIn)
		c.deviceCode = ""
	}
	return res, nil
}

// LoggedIn 返回是否已有有效 token（粗略判断，实际请求时 SDK 会自动刷新）。
func (c *Client) LoggedIn() bool {
	t, _ := c.store.GetAccessToken()
	return t != ""
}
