package main

import (
	middlewares "Aliddns-Ros/log-handler"
	"bytes"
	"crypto/tls"
	"fmt"
	"github.com/denverdino/aliyungo/dns"
	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/linxlib/conv"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"strings"
	"time"
)

// ConfigInfo 定义域名相关配置信息
type ConfigInfo struct {
	AccessKeyID     string
	AccessKeySecret string
	DomainName      string
	RR              string
	IpAddr          string
}

var (
	USAGE = `
详细参考 https://github.com/kirileec/Aliddns-Ros

1. 调用aliyun ddns更新dns记录. 
GET /aliddns?AccessKeyID=&AccessKeySecret=&DomainName=&RR=&IpAddr=&rt=6

2. 推送bark消息
POST /synobridge/bark?text=&device_key=&group=&category=

或者form参数传递

3. 推送telegram信息
POST /synobridge/tg?text=&title=&chat_id=&token=

或者form参数传递
`
)

func main() {
	fmt.Println("testing gstatic.com 204 status code")
	resp, err := http.Get("http://www.gstatic.com/generate_204")
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(resp.StatusCode, resp.Status)
	}
	r := gin.Default()
	r.Use(middlewares.Logger())
	r.GET("/", func(context *gin.Context) {
		context.Writer.WriteString(USAGE)
	})
	r.GET("/aliddns", AddUpdateAliddns)
	r.POST("/synobridge/:sendtype", SynologyBridge)
	r.Run(":8800")
}

func SynologyBridge(c *gin.Context) {
	var sendtype = c.Param("sendtype")
	switch sendtype {
	case "bark":
		text := c.PostForm("text")
		if strings.TrimSpace(text) == "" {
			text = c.Query("text")
		}
		deviceKey := c.PostForm("device_key")
		if strings.TrimSpace(deviceKey) == "" {
			deviceKey = c.Query("device_key")
		}
		title := c.PostForm("title")
		if strings.TrimSpace(title) == "" {
			title = c.Query("title")
		}
		group := c.PostForm("group")
		if strings.TrimSpace(group) == "" {
			group = c.Query("group")
		}
		category := c.PostForm("category")
		if strings.TrimSpace(category) == "" {
			category = c.Query("category")
		}
		text = strings.ReplaceAll(text, "\n", "\\n")
		log.Println("text: ", text)
		log.Println("device_key: ", deviceKey)
		log.Println("title: ", title)
		log.Println("group: ", group)
		log.Println("category: ", category)

		if text != "" && deviceKey != "" {
			json := []byte(fmt.Sprintf(`{"body": "%s","device_key": "%s","title": "%s", "group": "%s","category": "%s"}`, text, deviceKey, title, group, category))
			body := bytes.NewBuffer(json)

			// Create client
			client := &http.Client{
				Timeout: 10 * time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			}

			// Create request
			req, err := http.NewRequest("POST", "https://api.day.app/push", body)
			if err != nil {
				log.Println("Failure : ", err)
				return
			}

			// Headers
			req.Header.Add("Content-Type", "application/json; charset=utf-8")

			// Fetch Request
			resp, err := client.Do(req)

			if err != nil {
				log.Println("Failure : ", err)
				return
			}

			// Read Response Body
			respBody, _ := io.ReadAll(resp.Body)

			// Display Results
			log.Println("response Status : ", resp.Status)
			log.Println("response Headers : ", resp.Header)
			log.Println("response Body : ", string(respBody))
		}
	case "tg":
		text := c.PostForm("text")
		if strings.TrimSpace(text) == "" {
			text = c.Query("text")
		}
		chat_id := c.PostForm("chat_id")
		if strings.TrimSpace(chat_id) == "" {
			chat_id = c.Query("chat_id")
		}
		title := c.PostForm("title")
		if strings.TrimSpace(title) == "" {
			title = c.Query("title")
		}
		token := c.PostForm("token")
		if strings.TrimSpace(token) == "" {
			token = c.Query("token")
		}

		//category := c.PostForm("category")
		text = strings.ReplaceAll(text, "\n", "\\n")
		log.Println("text: ", text)
		log.Println("chat_id: ", chat_id)
		log.Println("title: ", title)
		log.Println("token: ", token)
		//log.Println("category: ", category)

		bot, err := tgbotapi.NewBotAPI(token)
		if err != nil {
			log.Println(err)
			break
		}
		//bot.SetAPIEndpoint("https://tgbot.202816.xyz/bot%s/%s")
		msg := tgbotapi.NewMessage(conv.Int64(chat_id), "*"+title+"*\n----------\n"+text)
		msg.ParseMode = "Markdown"
		m, err := bot.Send(msg)
		if err != nil {
			log.Println("Failure : ", err)
			break
		}
		log.Println(m.Text)
	}

}

func AddUpdateAliddns(c *gin.Context) {

	// 读取获取配置信息
	conf := new(ConfigInfo)
	conf.AccessKeyID = c.Query("AccessKeyID")
	conf.AccessKeySecret = c.Query("AccessKeySecret")
	conf.DomainName = c.Query("DomainName")
	conf.RR = c.Query("RR")
	conf.IpAddr = c.Query("IpAddr")
	var rt = c.Query("rt")

	if conf.RR == "" {
		var i = strings.Index(conf.DomainName, ".")
		conf.RR = conf.DomainName[:i]
		conf.DomainName = conf.DomainName[i+1:]
	}

	log.Println("当前路由公网IP：" + conf.IpAddr)
	log.Println("进行阿里云登录……")

	// 连接阿里云服务器，获取DNS信息
	client := dns.NewClient(conf.AccessKeyID, conf.AccessKeySecret)
	client.SetDebug(false)
	domainInfo := new(dns.DescribeDomainRecordsArgs)
	domainInfo.DomainName = conf.DomainName
	oldRecord, err := client.DescribeDomainRecords(domainInfo)
	if err != nil {
		log.Println("阿里云登录失败！请查看错误日志！", err)
		c.String(http.StatusOK, "loginerr")
		return
	}
	log.Println("阿里云登录成功！")
	log.Println("进行域名及IP比对……")

	var exsitRecordID string
	for _, record := range oldRecord.DomainRecords.Record {
		if record.DomainName == conf.DomainName && record.RR == conf.RR {
			if record.Value == conf.IpAddr {
				log.Println("当前配置解析地址与公网IP相同，不需要修改。")
				c.String(http.StatusOK, "same")
				return
			}
			exsitRecordID = record.RecordId
		}
	}

	if 0 < len(exsitRecordID) {
		// 有配置记录，则匹配配置文件，进行更新操作
		updateRecord := new(dns.UpdateDomainRecordArgs)
		updateRecord.RecordId = exsitRecordID
		updateRecord.RR = conf.RR
		updateRecord.Value = conf.IpAddr
		if rt == "6" {
			updateRecord.Type = dns.AAAARecord
		} else {
			updateRecord.Type = dns.ARecord
		}

		rsp := new(dns.UpdateDomainRecordResponse)
		rsp, err := client.UpdateDomainRecord(updateRecord)
		if nil != err {
			log.Println("修改解析地址信息失败!", err)
			c.String(http.StatusOK, "iperr")
		} else {
			log.Println("修改解析地址信息成功!", rsp)
			c.String(http.StatusOK, "ip")
		}
	} else {
		// 没有找到配置记录，那么就新增一个
		newRecord := new(dns.AddDomainRecordArgs)
		newRecord.DomainName = conf.DomainName
		newRecord.RR = conf.RR
		newRecord.Value = conf.IpAddr

		if rt == "6" {
			newRecord.Type = dns.AAAARecord
		} else {
			newRecord.Type = dns.ARecord
		}

		rsp := new(dns.AddDomainRecordResponse)
		rsp, err = client.AddDomainRecord(newRecord)
		if nil != err {
			log.Println("添加新域名解析失败！", err)
			c.String(http.StatusOK, "domainerr")
		} else {
			log.Println("添加新域名解析成功！", rsp)
			c.String(http.StatusOK, "domain")
		}
	}
}
