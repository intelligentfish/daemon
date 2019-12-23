package main

import (
	"context"
	"daemon"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
)

func main() {
	daemon := daemon.Default()
	daemon.Bootstrap(map[string]int{
		"web": 10080,
	}, func(tcpFds map[string]int,
		ready chan bool,
		exitCh chan interface{}) {
		engine := gin.Default()
		engine.GET("/pid", func(ctx *gin.Context) {
			ctx.String(http.StatusOK, fmt.Sprintf("pid:%d\n", os.Getpid()))
		})

		// 模拟耗时的操作...
		time.Sleep(1 * time.Second)

		// 获取文件描述符
		fd := tcpFds["web"]
		f := os.NewFile(uintptr(fd), "web")
		listener, err := net.FileListener(f)
		if err != nil {
			ready <- false
			glog.Error(err)
			return
		}
		defer listener.Close()

		// 创建HTTP服务器
		srv := &http.Server{Handler: engine}
		defer srv.Close()

		// 准备好
		ready <- true

		// 等待服务退出
		go func() {
			<-exitCh
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				glog.Error("server Shutdown: ", err)
			}
		}()

		// 开启HTTP服务器
		if err = srv.Serve(listener); nil != err {
			glog.Error(err)
		}

		glog.Error("app exited")
	})
}
