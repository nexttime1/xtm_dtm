package driver

import (
	"context"
	"fmt"
	consulAPI "github.com/hashicorp/consul/api" // Consul原生客户端
	"github.com/nexttime1/xtm_dtm/gnova/registry"
	"github.com/nexttime1/xtm_dtm/gnova/registry/consul"
	"github.com/nexttime1/xtm_dtm/gnova/registry/etcd"
	_ "github.com/nexttime1/xtm_dtm/gnova/rpcserver/resolver/direct"
	"github.com/nexttime1/xtm_dtm/gnova/rpcserver/resolver/discovery"
	etcdAPI "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/resolver" // gRPC地址解析器接口
	"net/url"                         // URL解析工具
	"strings"                         // 字符串处理
)

// 常量定义：驱动名和协议名
const (
	DriverName    = "dtm-driver-xtm" // 自定义驱动的名称（唯一标识）
	DefaultScheme = "discovery"      // 默认
	EtcdScheme    = "etcd"
	ConsulScheme  = "consul"
)

// kratosDriver：实现DTM Driver接口的结构体
type xtmDriver struct {
	consulAddr   string // Consul地址
	consulScheme string // Consul协议
}

func NewXtmDriver(consulAddr, consulScheme string) *xtmDriver {
	return &xtmDriver{
		consulAddr:   consulAddr,
		consulScheme: consulScheme,
	}
}

// GetName 返回驱动名称
// 对应dtmdriver.Register()的 key
func (k *xtmDriver) GetName() string {
	return DriverName // 返回 dtm-driver-xtm
}

// RegisterAddrResolver 注册地址解析器（DTM驱动接口要求）
// 作用：让DTM能识别并解析自定义协议（比如discovery://）
// 这个实现是空的
func (k *xtmDriver) RegisterAddrResolver() {
	// 空实现原因：Kratos的resolver在RegisterService中通过resolver.Register()注册，无需在这里重复注册
}

// RegisterService 注册服务实例到注册中心（DTM驱动接口要求）
// 作用：
// 1. 解析target（比如"discovery:///xshop-inventory-srv"）；
// 2. 根据协议（etcd/consul/discovery）创建Kratos注册中心客户端；
// 3. 注册Kratos的resolver，让DTM能解析discovery://协议；
// 4. 把服务实例注册到注册中心（可选，如果你需要DTM自身注册服务）。
// 参数：
// - target：服务地址（比如"discovery:///xshop-inventory-srv"）；
// - endpoint：服务端点（比如"grpc://127.0.0.1:8081"）。
func (k *xtmDriver) RegisterService(target string, endpoint string) error {
	// 1. 如果target为空，直接返回
	if target == "" {
		return nil
	}

	// 2. 解析target为URL对象（比如把"discovery:///xshop-inventory-srv"解析成URL结构体）
	u, err := url.Parse(target)
	if err != nil {
		return err // 解析失败返回错误
	}

	// 3. 根据URL的Scheme（协议）分支处理
	switch u.Scheme {
	// 处理discovery://或etcd://协议
	case DefaultScheme: // discovery://
		fallthrough // 穿透到ConsulScheme

	// 处理consul://协议
	case ConsulScheme: // consul://
		// 1. 创建Consul客户端（和你的NewRegistrar逻辑完全一致）
		c := consulAPI.DefaultConfig()
		c.Address = k.consulAddr  // 我们环境变量的
		c.Scheme = k.consulScheme // 我们环境变量的
		cli, err := consulAPI.NewClient(c)
		if err != nil {
			return err
		}

		// 封装成我们的Consul注册器
		consulRegistry := consul.New(cli, consul.WithHealthCheck(true))

		// 注册我们的discovery resolver 即使项目有注册，跟这个没关系，相当于不同进程了
		// 内部逻辑其实是走grpc 然后走 grpc 的服务发现 由于这里注册了自定义的，所以执行这里的逻辑，然后拿到服务实例，再给k8s内部节点通信
		resolver.Register(discovery.NewBuilder(consulRegistry, discovery.WithInsecure(true)))

		// 服务已经注册到Consul了，不需要DTM再注册
		// 直接返回nil，只完成resolver注册
		return nil

	case EtcdScheme: // etcd://
		// 构建服务实例对象
		registerInstance := &registry.ServiceInstance{
			Name:      strings.TrimPrefix(u.Path, "/"), // 服务名（比如xshop-inventory-srv）
			Endpoints: strings.Split(endpoint, ","),    // 服务端点（拆分多个地址）
		}

		client, err := etcdAPI.New(etcdAPI.Config{
			Endpoints: strings.Split(u.Host, ","),
		})
		if err != nil {
			return err // Etcd客户端创建失败
		}

		registry := etcd.New(client)

		resolver.Register(discovery.NewBuilder(registry, discovery.WithInsecure(true)))

		return registry.Register(context.Background(), registerInstance)

	default:
		return fmt.Errorf("unknown scheme: %s", u.Scheme)
	}
}

// ParseServerMethod 解析URI，拆分“服务地址”和“方法名”（DTM驱动接口要求）
// 作用：DTM调用服务时，需要把完整URI拆分成“服务地址”和“方法名”，比如：
// 输入："discovery:///xshop-inventory-srv/Inventory/Sell"
// 输出：server="discovery:///xshop-inventory-srv", method="/Inventory/Sell"
func (k *xtmDriver) ParseServerMethod(uri string) (server string, method string, err error) {
	// 保留Kratos原版：处理直连地址（如127.0.0.1:8081/Inventory/Sell）
	fmt.Println("ParseServerMethod 调用  url: ", uri)
	if !strings.Contains(uri, "//") {
		sep := strings.IndexByte(uri, '/')
		if sep == -1 {
			return "", "", fmt.Errorf("bad url: '%s'. no '/' found", uri)
		}
		return uri[:sep], uri[sep:], nil
	}

	//  解析失败返回具体错误（而非nil）
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", fmt.Errorf("parse consul discovery uri %s failed: %v", uri, err)
	}

	cleanPath := strings.TrimPrefix(u.Path, "/")
	pathParts := strings.SplitN(cleanPath, "/", 2)
	if len(pathParts) < 1 {
		return "", "", fmt.Errorf("consul discovery url %s missing service name", uri)
	}
	if len(pathParts) < 2 {
		return "", "", fmt.Errorf("consul discovery url %s missing method (e.g. /Inventory/Sell)", uri)
	}

	server = fmt.Sprintf("%s:///%s", u.Scheme, pathParts[0])

	// method 保持 /Inventory/Sell
	method = "/" + pathParts[1]
	return server, method, nil

}
