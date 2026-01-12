## 项目说明

提供简单的接口调用阿里云DNS服务，实现动态域名解析。方便让类似RouterOS的系统去更新DDNS.

### v2 变动

- AK/SK 修改为环境变量配置
- 升级使用alidns v2 SDK
- 推送到 ghcr, docker hub保留v1版本
- 记录域名的变更记录, 并提供接口查询

## Docker部署

docker compose 
```yaml
services:
  aliddns-ros:
    image: ghcr.io/kirileec/aliddnsv2:latest
    # image: slk1133/aliddnsv2:latest
    container_name: aliddns-ros
    restart: always
    environment:
        ALIDNS_ACCESS_KEY_ID: "xxxx"
        ALIDNS_ACCESS_KEY_SECRET: "xxxx"
        RECORD_CHANGES: true
```




# 二、RouterOS7.x 脚本代码

## ROS路由脚本(A记录, IPV4)  

```
#xxxx处替换为需要解析的域名，如baidu.com  
:local DomainName "lsprain.xxxx"  
#xxxx处替换为需要解析的子域名，如home.baidu.com只需要填home即可   
:local RR "home"   
#xxxx处替换为网络出口名称，如pppoe-out1  
:local pppoe "pppoe-out1"   

:local IpAddr [/ip address get [/ip address find interface=$pppoe] address]  
:set IpAddr [:pick $IpAddr 0 ([len $IpAddr] -3)]  
:log warning "当前公网IP地址：$IpAddr"  

:local aliddns "http://服务地址:8800/aliddns?RR=$RR&DomainName=$DomainName&IpAddr=$IpAddr"  

:local result [/tool fetch url=("$aliddns") mode=http http-method=get as-value output=user];  
#:log warning $result  

:if ($result->"status" = "finished") do={  

:if ($result->"data" = "loginerr") do={  
:log warning "阿里云登录失败！!";  
}  
:if ($result->"data" = "iperr") do={  
:log warning "修改解析地址信息失败!";  
}  
:if ($result->"data" = "ip") do={  
:log warning "修改解析地址信息成功!";  
}  
:if ($result->"data" = "domainerr") do={  
:log warning "添加新域名解析失败!";  
}  
:if ($result->"data" = "domain") do={  
:log warning "添加新域名解析成功!";  
}  
:if ($result->"data" = "same") do={  
:log warning "当前配置解析地址与公网IP相同，不需要修改!";  
}  
:if ($result->"data" = "ip") do={  
:log warning "更新IP信息成功!";  
:log warning "$IpAddr!";  
}  
:if ($result->"data" = "domain") do={  
:log warning "增加域名信息成功!";  
}  
}  
}  
```
## ROS路由脚本(AAAA记录, IPV6)

```
#xxxx处替换为需要解析的域名，如baidu.com  
:local DomainName "baidu.com"  
#xxxx处替换为需要解析的子域名，如home.baidu.com只需要填home即可   
:local RR "ros6"   
#xxxx处替换为网络出口名称，如pppoe-out1  
:local pppoe "pppoe-cm"   

:local IpAddr [/ipv6 address get [find interface=br-lan advertise=yes] address]
:set IpAddr [:pick $IpAddr 0 [:find $IpAddr "/"]]  
:log warning "当前公网IPv6地址：$IpAddr"  

:local aliddns "https://自建服务地址/aliddns?RR=$RR&DomainName=$DomainName&rt=6&IpAddr=$IpAddr"  

:local result [/tool fetch url=("$aliddns") mode=http http-method=get as-value output=user];  
#:log warning $result  

:if ($result->"status" = "finished") do={  

:if ($result->"data" = "loginerr") do={  
:log warning "阿里云登录失败！!";  
}  
:if ($result->"data" = "iperr") do={  
:log warning "修改解析地址信息失败!";  
}  
:if ($result->"data" = "ip") do={  
:log warning "修改解析地址信息成功!";  
}  
:if ($result->"data" = "domainerr") do={  
:log warning "添加新域名解析失败!";  
}  
:if ($result->"data" = "domain") do={  
:log warning "添加新域名解析成功!";  
}  
:if ($result->"data" = "same") do={  
:log warning "当前配置解析地址与公网IP相同，不需要修改!";  
}  
:if ($result->"data" = "ip") do={  
:log warning "更新IP信息成功!";  
:log warning "$IpAddr!";  
}  
:if ($result->"data" = "domain") do={  
:log warning "增加域名信息成功!";  
}  
}  
}  
```

## 群晖DDNS Query URL

```
http://服务地址:8800/aliddns?RR=&DomainName=dsm.example.com&IpAddr=XXX&rt=4
```

注: 对于群晖的DDNS增加了支持. 即当RR参数为空时, 从DomainName参数中截取子域名和根域名再发送给aliyun.

`DomainName = dsm.example.com` 会被解析为  `RR = dsm`  `DomainName = example.com`

# 三、其它方式
请求方法：`GET`

请求地址：
```
http://服务地址:8800/aliddns?RR=XX&DomainName=XXX&IpAddr=XXX&rt=4
```

## 参数说明

- RR: 子域名
- DomainName: 主域名
- IpAddr: 要更新的DNS记录地址
- rt: 4/6 表示是IPv4记录或者IPv6记录
