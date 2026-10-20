package fnos

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client 是飞牛影视 Web API 客户端。
type Client struct {
	Base, User, Pass, Secret, APIKey string
	http *http.Client
	mu sync.Mutex
	token string
	expires time.Time
}

type Item struct {
	GUID string `json:"guid"`
	ParentGUID string `json:"parent_guid"`
	AncestorGUID string `json:"ancestor_guid"`
	Title string `json:"title"`
	TVTitle string `json:"tv_title"`
	Type string `json:"type"`
	Season int `json:"season_number"`
	Episode int `json:"episode_number"`
	NumberOfEpisodes int `json:"number_of_episodes"`
	FileName string `json:"file_name"`
	Path string `json:"path"`
}

type response struct { Code int `json:"code"`; Msg string `json:"msg"`; Data json.RawMessage `json:"data"` }
type listData struct { List []Item `json:"list"`; Total int `json:"total"` }

func New(base,user,pass,secret,key string) *Client {
	return &Client{Base:strings.TrimRight(strings.TrimSpace(base),"/"),User:user,Pass:pass,Secret:secret,APIKey:key,http:&http.Client{Timeout:25*time.Second}}
}
func (c *Client) Configured() bool { return c!=nil && c.Base!="" && c.User!="" && c.Pass!="" && c.Secret!="" && c.APIKey!="" }

func md5hex(s []byte) string { x:=md5.Sum(s); return hex.EncodeToString(x[:]) }

// pythonJSON 兼容飞牛 Web 端 authx 的请求体格式：字段按结构体顺序，
// 冒号和逗号后带空格。签名是请求体字节级 MD5，不能直接用 Go 的紧凑 JSON。
func pythonJSON(v any) ([]byte,error) {
	b,err:=json.Marshal(v); if err!=nil{return nil,err}
	out:=make([]byte,0,len(b)+8); quoted,escape:=false,false
	for _,ch:=range b {
		if quoted {
			out=append(out,ch); if escape { escape=false } else if ch=='\\' { escape=true } else if ch=='"' { quoted=false }; continue
		}
		if ch=='"' { quoted=true; out=append(out,ch); continue }
		out=append(out,ch); if ch==':'||ch==',' { out=append(out,' ') }
	}
	return out,nil
}
func (c *Client) authx(path string, body []byte) string {
	nonce:=strconv.Itoa(100000+rand.Intn(900000)); ts:=strconv.FormatInt(time.Now().UnixMilli(),10)
	return "nonce="+nonce+"&timestamp="+ts+"&sign="+md5hex([]byte(c.Secret+"_"+path+"_"+nonce+"_"+ts+"_"+md5hex(body)+"_"+c.APIKey))
}
func (c *Client) do(ctx context.Context, method,path string, payload any, token string) (json.RawMessage,error) {
	body:=[]byte{}
	if payload!=nil { var err error; body,err=pythonJSON(payload); if err!=nil{return nil,err} }
	full:="/v/api/v1"+path
	req,err:=http.NewRequestWithContext(ctx,method,c.Base+full,bytes.NewReader(body));if err!=nil{return nil,err}
	req.Header.Set("Content-Type","application/json"); req.Header.Set("authx",c.authx(full,body)); if token!=""{req.Header.Set("Authorization",token)}
	resp,err:=c.http.Do(req);if err!=nil{return nil,err};defer resp.Body.Close();raw,err:=io.ReadAll(io.LimitReader(resp.Body,16<<20));if err!=nil{return nil,err}
	var r response;if err=json.Unmarshal(raw,&r);err!=nil{return nil,fmt.Errorf("飞牛响应解析失败: %w",err)}
	if r.Code!=0{return nil,fmt.Errorf("飞牛 API %s: %s",path,r.Msg)};return r.Data,nil
}
func (c *Client) login(ctx context.Context)(string,error){
	c.mu.Lock();defer c.mu.Unlock();if c.token!=""&&time.Now().Before(c.expires){return c.token,nil}
	data,err:=c.do(ctx,"POST","/login",struct{ Username string `json:"username"`; Password string `json:"password"`; AppName string `json:"app_name"` }{c.User,c.Pass,"trimemedia-we"},"");if err!=nil{return "",fmt.Errorf("飞牛登录失败: %w",err)}
	var d struct{Token string `json:"token"`};if json.Unmarshal(data,&d)!=nil||d.Token==""{return "",fmt.Errorf("飞牛登录响应没有 token")};c.token=d.Token;c.expires=time.Now().Add(40*time.Hour);return c.token,nil
}
func parseGUID(raw string) string { u,err:=url.Parse(raw);if err!=nil{return ""};parts:=strings.Split(strings.Trim(u.Path,"/"),"/");for i:=len(parts)-1;i>=0;i--{if len(parts[i])==32{return parts[i]}};return "" }

func (c *Client) listChildren(ctx context.Context, tok, guid string) ([]Item,error) {
	var out []Item
	for page:=1;page<=100;page++ {
		data,err:=c.do(ctx,"POST","/item/list",struct{ ParentGUID string `json:"parent_guid"`; Page int `json:"page"`; PageSize int `json:"page_size"` }{guid,page,500},tok)
		if err!=nil{return nil,err};var d listData;if err=json.Unmarshal(data,&d);err!=nil{return nil,err}
		out=append(out,d.List...);if len(d.List)==0||len(out)>=d.Total{break}
	}
	return out,nil
}

// Episodes 支持两种飞牛地址：/v/tv/season/{季ID} 与 /v/tv/{剧ID}。
// 整部剧地址会先找到订阅指定的季节点，再读取该季的 Episode 子项。
func (c *Client) Episodes(ctx context.Context, mediaURL string, wantedSeason int) ([]Item,error) {
	if !c.Configured(){return nil,fmt.Errorf("飞牛影视未配置完整")};tok,err:=c.login(ctx);if err!=nil{return nil,err};guid:=parseGUID(mediaURL);if guid==""{return nil,fmt.Errorf("无法从飞牛影视地址提取 ID")}
	items,err:=c.listChildren(ctx,tok,guid);if err!=nil{return nil,err}
	// 整部剧 URL 的直接子项是 Season，继续展开匹配的季。
	var seasons []Item
	for _,it:=range items { if strings.EqualFold(it.Type,"Season") { seasons=append(seasons,it) } }
	if len(seasons)>0 {
		var chosen *Item
		for i:=range seasons { if seasons[i].Season==wantedSeason { chosen=&seasons[i];break } }
		if chosen==nil && len(seasons)==1 { chosen=&seasons[0] }
		if chosen==nil { return nil,fmt.Errorf("飞牛影视中找不到第%d季",wantedSeason) }
		return c.listChildren(ctx,tok,chosen.GUID)
	}
	return items,nil
}
func (c *Client) Test(ctx context.Context) error { _,err:=c.login(ctx);return err }
