package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"github.com/campusos/CampusOS/internal/platform/webcompanion"
	"os"
	"os/signal"
	"strings"
	"sync"
)

func main() {
	server := flag.String("server", "", "CampusOS HTTPS 地址，或可信局域网 HTTP IP 地址")
	name, _ := os.Hostname()
	if name == "" {
		name = "我的电脑"
	}
	label := flag.String("name", name, "页面显示的电脑名称")
	proxy := flag.String("proxy", os.Getenv("CAMPUSOS_COMPANION_PROXY"), "仅本机抓取使用的 HTTP/HTTPS/SOCKS5 代理；留空直连")
	flag.Parse()
	input := bufio.NewReader(os.Stdin)
	if *server == "" {
		fmt.Print("CampusOS 地址：")
		line, _ := input.ReadString('\n')
		*server = strings.TrimSpace(line)
	}
	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "proxy" {
			explicit = true
		}
	})
	if !explicit && *proxy == "" {
		fmt.Print("本机抓取代理（留空直连，例如 http://127.0.0.1:7890 或 socks5://127.0.0.1:7891）：")
		line, _ := input.ReadString('\n')
		*proxy = strings.TrimSpace(line)
	}
	client, err := webcompanion.NewClientWithProxy(*server, *proxy)
	if err != nil {
		fmt.Fprintln(os.Stderr, "地址或本机代理配置无效。不会修改系统、浏览器或服务器代理。")
		os.Exit(1)
	}
	fmt.Println("本机抓取网络：" + client.ProxyMode + "；此设置仅在当前助手进程中使用，地址和口令不上传。")
	fmt.Print("请在插件界面点击“连接本机助手”，将一次性配对码粘贴到这里：")
	code, _ := input.ReadString('\n')
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err = client.Connect(ctx, strings.TrimSpace(code), strings.TrimSpace(*label)); err != nil {
		fmt.Fprintln(os.Stderr, "配对失败，请检查本站地址和网络，并生成新的配对码。")
		os.Exit(1)
	}
	fmt.Println("本机助手已连接。回到插件界面明确选择这台电脑；保持窗口开启，Ctrl+C 退出。")
	var mu sync.Mutex
	err = client.Run(ctx, func(message string) { mu.Lock(); defer mu.Unlock(); fmt.Println(message) })
	if ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "连接已断开或过期，请重新配对。")
		os.Exit(1)
	}
}
