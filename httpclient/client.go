package httpclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

var (
	// timeZone is used for set DataTime in HealthForm,
	// default: CST(China Standard Time)
	timeZone = time.FixedZone("CST", 8*3600)
)

// LoginConfirm 验证账号密码
func LoginConfirm(ctx context.Context, account [2]string, timeout time.Duration) error {
	var cc context.CancelFunc
	ctx, cc = context.WithTimeout(ctx, timeout)
	_, err := login(ctx, account)
	cc()
	return parseURLError(err)
}

// Punch 打卡
func Punch(ctx context.Context, account [2]string, timeout time.Duration) (err error) {
	var cc context.CancelFunc
	ctx, cc = context.WithTimeout(ctx, timeout)
	defer cc()

	defer func() {
		err = parseURLError(err)
	}()

	var cookies []*http.Cookie
	cookies, err = login(ctx, account) // 登录，获取cookie
	if err != nil {
		return
	}

	cookies, err = getFormSessionID(ctx, cookies) // 获取打卡系统的cookie
	if err != nil {
		return
	}

	var (
		form   *HealthForm
		params *QueryParam
	)
	form, params, err = getFormDetail(ctx, cookies) // 获取打卡列表信息
	if err != nil {
		return err
	}

	err = postForm(ctx, form, params, cookies) // 提交表单
	return
}

// SetTimeZone 设置时区
func SetTimeZone(tz *time.Location) {
	if tz != nil {
		timeZone = tz
	}
}

// parseURLError 解析URL错误
func parseURLError(err error) error {
	if v, ok := err.(*url.Error); ok {
		err = v.Err
	}
	return err
}
