
FROM golang:alpine AS build

RUN apk --no-cache add tzdata

# 设置当前工作区
WORKDIR /app

# 把全部文件添加到/go/release目录
COPY . .

# 编译: 把main.go编译为可执行的二进制文件, 并命名为app
RUN GOOS=linux CGO_ENABLED=0 GOARCH=amd64 go build -ldflags="-s -w" -installsuffix cgo -o main main.go

# 运行: 使用scratch作为基础镜像
FROM scratch as prod
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# 在build阶段, 复制时区配置到镜像的/etc/localtime
COPY --from=build /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

# 在build阶段, 复制./app目录下的可执行二进制文件到当前目录
COPY --from=build /app/main /app/main

EXPOSE 8800

# 启动服务
CMD ["/app/main"]