package httpclient

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/go-querystring/query"
)

// ErrCouldNotGetFormSession get form session id failed
var ErrCouldNotGetFormSession = errors.New("could not get form session")

type htmlSymbol uint8

const (
	symbolJSON htmlSymbol = iota
	symbolString
)

const reportDomains = "dailyreport.hhu.edu.cnform.hhu.edu.cn"

var (
	//ErrCannotParseData cannot parse html data error
	ErrCannotParseData = errors.New("data: parse error")

	timeZone = time.FixedZone("CST", 8*3600)
)

// getFormSessionID 获取打卡系统的SessionID
func (c *punchClient) getFormSessionID() (path string, err error) {
	var req *http.Request
	req, err = getWithContext(c.ctx, "http://"+c.site+"/pdc/form/list")
	if err != nil {
		return
	}

	var res *http.Response
	if res, err = c.httpClient.Do(req); err != nil {
		return
	}

	bufferReader := bufio.NewReader(res.Body)

	for err == nil && !strings.HasPrefix(path, `<a href="/pdc/formDesignApi/S/`) {
		path, err = scanLine(bufferReader)
	}
	drainBody(res.Body)

	if path != "" {
		var data []byte
		data, err = parseData(path, symbolString)
		path = string(data)
	}

	if err == nil && c.httpClient.Jar.Cookies(&url.URL{Host: c.site}) == nil {
		err = ErrCouldNotGetFormSession
	}
	if err != nil {
		path = ""
	}
	return
}

// getFormDetail 获取打卡表单详细信息
func (c *punchClient) getFormDetail(path string) (form map[string]string, params *QueryParam, err error) {
	var req *http.Request
	req, err = getWithContext(c.ctx, "http://"+c.site+path)
	if err != nil {
		return
	}

	var res *http.Response
	if res, err = c.httpClient.Do(req); err != nil {
		return
	}

	var (
		bufferReader  = bufio.NewReader(res.Body)
		wid, formData []byte
		line          string
	)

	for err == nil {
		line, err = scanLine(bufferReader)
		if strings.HasPrefix(line, "var _selfFormWid") {
			wid, err = parseData(line, symbolString)
			break
		}
	}
	for err == nil {
		line, err = scanLine(bufferReader)
		if strings.HasPrefix(line, "fillDetail") {
			formData, err = parseData(line, symbolJSON)
			break
		}
	}
	drainBody(res.Body)

	if err != nil {
		return
	}

	tmpForm := make(map[string]string)
	if err = json.Unmarshal(formData, &tmpForm); err != nil {
		return
	}

	if err = zeroValueCheck(tmpForm); err != nil {
		return
	}
	tmpForm["DATETIME_CYCLE"] = time.Now().In(timeZone).Format("2006/01/02") // 表单中增加打卡日期

	form = tmpForm
	params = &QueryParam{
		Wid:    string(wid),
		UserID: form["USERID"],
	}

	delete(tmpForm, "CLRQ")   // 删除填报时间字段
	delete(tmpForm, "USERID") // 删除UserID字段
	return
}

// postForm 提交打卡表单
func (c *punchClient) postForm(form map[string]string, params *QueryParam) error {
	value := make(url.Values, len(form))
	for key, val := range form {
		value.Set(key, val)
	}

	req, err := postFormWithContext(c.ctx,
		"http://"+c.site+"/pdc/formDesignApi/dataFormSave",
		value,
	)
	if err != nil {
		return err
	}

	value, err = query.Values(params)
	if err != nil {
		return err
	}

	req.URL.RawQuery = value.Encode()

	var res *http.Response
	if res, err = c.httpClient.Do(req); err != nil {
		return err
	}
	drainBody(res.Body)

	if res.StatusCode != http.StatusOK {
		return errors.New("post failed, status: " + res.Status)
	}
	return nil
}

func parseData(data string, symbol htmlSymbol) (res []byte, err error) {
	switch symbol {
	case symbolJSON:
		res, err = getSlice(data, '{', '}', true)
	case symbolString:
		res, err = getSlice(data, '\'', '\'', false)
		if err != nil {
			res, err = getSlice(data, '"', '"', false)
		}
	default:
		err = errors.New("data: invalid symbol")
	}
	return
}

func getSlice(data string, startSymbol, endSymbol byte, containSymbol bool) ([]byte, error) {
	start := strings.IndexByte(data, startSymbol)
	if start == -1 {
		return nil, ErrCannotParseData
	}

	length := strings.IndexByte(data[start+1:], endSymbol)
	if length == -1 {
		return nil, ErrCannotParseData
	}

	if containSymbol {
		length += 2
	} else {
		start++
	}

	res := make([]byte, length)
	copy(res, data[start:]) // copy the sub string from data to res

	return res, nil
}

func zeroValueCheck(item map[string]string) error {
	if len(item) == 0 {
		return errors.New("check: the map is empty")
	}
	for key, value := range item {
		if value == "" {
			return errors.New("check: '" + key + "' has zero value")
		}
	}
	return nil
}
