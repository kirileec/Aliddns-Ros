package main

import (
	middlewares "Aliddns-Ros/log-handler"
	"bytes"
	"crypto/tls"
	"fmt"
	"github.com/denverdino/aliyungo/dns"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
)

func init() {

}

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
`
)

func main() {
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
		deviceKey := c.PostForm("device_key")
		title := c.PostForm("title")
		group := c.PostForm("group")
		category := c.PostForm("category")
		text = strings.ReplaceAll(text, `\n`, "%0a")
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
			respBody, _ := ioutil.ReadAll(resp.Body)

			// Display Results
			log.Println("response Status : ", resp.Status)
			log.Println("response Headers : ", resp.Header)
			log.Println("response Body : ", string(respBody))
		}

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
