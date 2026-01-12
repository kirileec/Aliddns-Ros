package main

import (
	middlewares "Aliddns-Ros/log-handler"
	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/aliyun/credentials-go/credentials"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"net/http"
	"os"
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
GET /aliddns?DomainName=&RR=&IpAddr=&rt=6
2. 查询变更记录
GET /changes?DomainName=example.com&RR=aa
`
)

var (
	//recordChanges = true
	recordChanges = strings.ToLower(os.Getenv("RECORD_CHANGES")) == "true"
	//recordChangesNotify = strings.ToLower(os.Getenv("RECORD_CHANGES_NOTIFY")) == "true"
	//tgBotToken          = os.Getenv("TG_BOT_TOKEN")
	//tgChatID            = os.Getenv("TG_BOT_CHAT_ID")
)

func main() {
	r := gin.Default()
	r.Use(middlewares.Logger())
	r.GET("/", func(context *gin.Context) {
		context.Writer.WriteString(USAGE)
	})
	r.GET("/aliddns", AddUpdateAliddns)
	if recordChanges {
		r.GET("/changes", ListDomainChanges)
	}
	r.Run(":8800")
}

type DomainChangeRecord struct {
	DomainName string
	RR         string
	IpAddr     string
	Time       time.Time
	Desc       string
}

var domainChangeRecords []DomainChangeRecord

func ListDomainChanges(c *gin.Context) {
	log.Println("获取域名解析记录变更列表")
	domainName := c.Query("DomainName")
	if domainName == "" {
		c.String(http.StatusOK, "param DomainName is empty")
		return
	}
	rr := c.Query("RR")
	var records []DomainChangeRecord
	for _, record := range domainChangeRecords {
		if rr == "" {
			if record.DomainName == domainName {
				records = append(records, record)
			}
		} else {
			if record.DomainName == domainName && record.RR == rr {
				records = append(records, record)
			}
		}
	}
	c.PureJSON(http.StatusOK, gin.H{
		"data": records,
	})
}

func AddUpdateAliddns(c *gin.Context) {
	// 读取获取配置信息
	conf := new(ConfigInfo)
	conf.AccessKeyID = os.Getenv("ALIDNS_ACCESS_KEY_ID")
	conf.AccessKeySecret = os.Getenv("ALIDNS_ACCESS_KEY_SECRET")
	if conf.AccessKeyID == "" || conf.AccessKeySecret == "" {
		c.String(http.StatusOK, "env ALIDNS_ACCESS_KEY_ID or ALIDNS_ACCESS_KEY_SECRET is empty")
		return
	}
	conf.DomainName = c.Query("DomainName")
	if conf.DomainName == "" {
		c.String(http.StatusOK, "param DomainName is empty")
		return
	}
	conf.RR = c.Query("RR")
	conf.IpAddr = c.Query("IpAddr")
	if conf.IpAddr == "" {
		c.String(http.StatusOK, "param IpAddr is empty")
		return
	}
	var rt = c.Query("rt")
	if rt == "" {
		rt = "4"
	}

	if conf.RR == "" {
		var i = strings.Index(conf.DomainName, ".")
		conf.RR = conf.DomainName[:i]
		conf.DomainName = conf.DomainName[i+1:]
	}

	log.Println("当前路由公网IP：" + conf.IpAddr)
	log.Println("进行阿里云登录……")

	// 连接阿里云服务器，获取DNS信息
	client, _ := createClient(conf.AccessKeyID, conf.AccessKeySecret)
	domainInfo := new(alidns.DescribeDomainRecordsRequest)
	domainInfo.SetDomainName(conf.DomainName)
	oldRecord, err := client.DescribeDomainRecords(domainInfo)
	if err != nil {
		log.Println("阿里云登录失败！请查看错误日志！", err)
		c.String(http.StatusOK, "loginerr")
		return
	}
	log.Println("阿里云登录成功！")
	log.Println("进行域名及IP比对……")

	var exsitRecordID string
	var oldIp string
	for _, record := range oldRecord.GetBody().DomainRecords.Record {
		if tea.StringValue(record.DomainName) == conf.DomainName && tea.StringValue(record.RR) == conf.RR {
			if tea.StringValue(record.Value) == conf.IpAddr {
				log.Println("当前配置解析地址与公网IP相同，不需要修改。")
				c.String(http.StatusOK, "same")
				return
			}
			exsitRecordID = tea.StringValue(record.RecordId)
			oldIp = tea.StringValue(record.Value)
		}
	}

	if 0 < len(exsitRecordID) {
		// 有配置记录，则匹配配置文件，进行更新操作
		updateRecord := new(alidns.UpdateDomainRecordRequest)
		updateRecord.RecordId = tea.String(exsitRecordID)
		updateRecord.RR = tea.String(conf.RR)
		updateRecord.Value = tea.String(conf.IpAddr)
		if rt == "6" {
			updateRecord.Type = tea.String("AAAA")
		} else {
			updateRecord.Type = tea.String("A")
		}

		rsp, err := client.UpdateDomainRecord(updateRecord)
		if nil != err {
			log.Println("修改解析地址信息失败!", err)
			c.String(http.StatusOK, "iperr")
		} else {
			log.Println("修改解析地址信息成功!", rsp)
			if recordChanges {
				domainChangeRecords = append(domainChangeRecords, DomainChangeRecord{
					DomainName: conf.DomainName,
					RR:         conf.RR,
					IpAddr:     conf.IpAddr,
					Time:       time.Now(),
					Desc:       "更新:" + oldIp + "->" + conf.IpAddr,
				})
			}

			c.String(http.StatusOK, "ip")
		}
	} else {
		// 没有找到配置记录，那么就新增一个
		newRecord := new(alidns.AddDomainRecordRequest)
		newRecord.DomainName = tea.String(conf.DomainName)
		newRecord.RR = tea.String(conf.RR)
		newRecord.Value = tea.String(conf.IpAddr)

		if rt == "6" {
			newRecord.Type = tea.String("AAAA")
		} else {
			newRecord.Type = tea.String("A")
		}

		rsp, err := client.AddDomainRecord(newRecord)
		if nil != err {
			log.Println("添加新域名解析失败！", err)
			c.String(http.StatusOK, "domainerr")
		} else {
			log.Println("添加新域名解析成功！", rsp)
			if recordChanges {
				domainChangeRecords = append(domainChangeRecords, DomainChangeRecord{
					DomainName: conf.DomainName,
					RR:         conf.RR,
					IpAddr:     conf.IpAddr,
					Time:       time.Now(),
					Desc:       "新增:->" + conf.IpAddr,
				})
			}

			c.String(http.StatusOK, "domain")
		}
	}
}

func createClient(ak string, sk string) (*alidns.Client, error) {
	config := new(credential.Config).SetType("access_key").SetAccessKeyId(ak).SetAccessKeySecret(sk)
	akCredential, err := credential.NewCredential(config)
	if err != nil {
		return nil, err
	}
	oConfig := &openapi.Config{
		Credential: akCredential,
	}
	oConfig.Endpoint = tea.String("alidns.aliyuncs.com")
	return alidns.NewClient(oConfig)
}
