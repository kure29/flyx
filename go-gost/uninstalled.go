package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// idleAfterUninstall keeps an uninstalled agent's container from crash-looping
// under its restart policy until someone removes the container.
func idleAfterUninstall() {
	fmt.Println("ℹ️ 面板已删除这台服务器并卸载了 agent，不再连接面板。请在宿主机上执行 docker rm -f 删除这个容器")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	os.Exit(0)
}
