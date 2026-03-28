package driver

import (
	"fmt"
	"github.com/dtm-labs/dtmdriver"
	"os"
)

func init() {
	// 1. 优先从环境变量获取，方便 K8s 部署时通过 env 注入
	// 在你的 dtm 部署 yaml 里添加 ENV: CONSUL_ADDRESS
	address := os.Getenv("CONSUL_ADDRESS")
	if address == "" {
		// 2. 如果没有环境变量，使用 K8s 内部 Consul 的 Service 域名
		address = "consul:8500"
	}

	scheme := os.Getenv("CONSUL_SCHEME")
	if scheme == "" {
		scheme = "http"
	}

	fmt.Printf("正在初始化 DTM 驱动，Consul 地址: %s", address)

	dtmDriver := NewXtmDriver(address, scheme)
	dtmdriver.Register(dtmDriver)

	err := dtmdriver.Use(DriverName)
	if err != nil {
		panic(fmt.Sprintf("激活DTM驱动失败: %v", err))
	}
	fmt.Println("激活DTM驱动成功")
}
