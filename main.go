package main

import (
	middlewares "Aliddns-Ros/log-handler"
	"fmt"
	alidns "github.com/alibabacloud-go/alidns-20150109/v5/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	credential "github.com/aliyun/credentials-go/credentials"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"net/http"
	"net/netip"
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
2. 查看变更记录
GET /changes
`
)

var recordChanges = strings.EqualFold(os.Getenv("RECORD_CHANGES"), "true")

func main() {
	var err error
	changeStore, err = NewChangeStore(os.Getenv("ALIDNS_DB_PATH"))
	if err != nil {
		log.Fatal("初始化变更记录数据库失败：", err)
	}
	defer changeStore.Close()

	r := gin.Default()
	r.Use(middlewares.Logger())
	r.GET("/", func(context *gin.Context) {
		context.Writer.WriteString(USAGE)
	})
	r.GET("/aliddns", AddUpdateAliddns)
	r.GET("/changes", ChangesPage)
	r.GET("/api/changes", ListDomainChanges)
	r.Run(":8800")
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
	var err error
	conf.IpAddr, err = normalizeIPAddress(conf.IpAddr, rt)
	if err != nil {
		log.Println("IP地址参数无效：", err)
		c.String(http.StatusOK, "param IpAddr is invalid")
		return
	}
	recordType := "A"
	if rt == "6" {
		recordType = "AAAA"
	}

	if conf.RR == "" {
		var i = strings.Index(conf.DomainName, ".")
		if i <= 0 || i == len(conf.DomainName)-1 {
			c.String(http.StatusOK, "param RR is empty")
			return
		}
		conf.RR = conf.DomainName[:i]
		conf.DomainName = conf.DomainName[i+1:]
	}

	log.Println("当前路由公网IP：" + conf.IpAddr)
	log.Println("进行阿里云登录……")

	// 连接阿里云服务器，获取DNS信息
	client, err := createClient(conf.AccessKeyID, conf.AccessKeySecret)
	if err != nil {
		log.Println("阿里云客户端创建失败！", err)
		c.String(http.StatusOK, "loginerr")
		return
	}
	domainInfo := new(alidns.DescribeDomainRecordsRequest)
	domainInfo.SetDomainName(conf.DomainName)
	domainInfo.SetRRKeyWord(conf.RR)
	domainInfo.SetType(recordType)
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
		if tea.StringValue(record.DomainName) == conf.DomainName &&
			tea.StringValue(record.RR) == conf.RR &&
			tea.StringValue(record.Type) == recordType {
			if sameIPAddress(tea.StringValue(record.Value), conf.IpAddr) {
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
		updateRecord.Type = tea.String(recordType)

		rsp, err := client.UpdateDomainRecord(updateRecord)
		if nil != err {
			log.Println("修改解析地址信息失败!", err)
			c.String(http.StatusOK, "iperr")
		} else {
			log.Println("修改解析地址信息成功!", rsp)
			saveDomainChange(DomainChangeRecord{
				DomainName: conf.DomainName,
				RR:         conf.RR,
				IpAddr:     conf.IpAddr,
				Time:       time.Now(),
				Desc:       "更新:" + oldIp + "->" + conf.IpAddr,
			})

			c.String(http.StatusOK, "ip")
		}
	} else {
		// 没有找到配置记录，那么就新增一个
		newRecord := new(alidns.AddDomainRecordRequest)
		newRecord.DomainName = tea.String(conf.DomainName)
		newRecord.RR = tea.String(conf.RR)
		newRecord.Value = tea.String(conf.IpAddr)
		newRecord.Type = tea.String(recordType)

		rsp, err := client.AddDomainRecord(newRecord)
		if nil != err {
			log.Println("添加新域名解析失败！", err)
			c.String(http.StatusOK, "domainerr")
		} else {
			log.Println("添加新域名解析成功！", rsp)
			saveDomainChange(DomainChangeRecord{
				DomainName: conf.DomainName,
				RR:         conf.RR,
				IpAddr:     conf.IpAddr,
				Time:       time.Now(),
				Desc:       "新增:->" + conf.IpAddr,
			})

			c.String(http.StatusOK, "domain")
		}
	}
}

func normalizeIPAddress(value string, rt string) (string, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || addr.Zone() != "" {
		return "", fmt.Errorf("%q 不是有效的IP地址", value)
	}

	switch rt {
	case "4":
		if !addr.Is4() {
			return "", fmt.Errorf("rt=4 需要IPv4地址")
		}
		return addr.String(), nil
	case "6":
		if !addr.Is6() || addr.Is4In6() {
			return "", fmt.Errorf("rt=6 需要IPv6地址")
		}
		// 使用完整的8组表示，避免上游RPC链路把IPv6前缀误判为可省略部分。
		return addr.StringExpanded(), nil
	default:
		return "", fmt.Errorf("rt只能是4或6")
	}
}

func sameIPAddress(left string, right string) bool {
	leftAddr, leftErr := netip.ParseAddr(left)
	rightAddr, rightErr := netip.ParseAddr(right)
	if leftErr != nil || rightErr != nil {
		return left == right
	}
	return leftAddr == rightAddr
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
