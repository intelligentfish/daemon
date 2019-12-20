//// +build !windows

package daemon

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/golang/glog"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
)

// 响应
const (
	ReadyOK    = "ReadyOK"
	ReadyError = "ReadyError"
)

// panicOnError 错误崩溃
func panicOnError(err error) {
	if nil != err {
		panic(err)
	}
}

// Daemon 守护进程
type Daemon struct {
	killedFlag      int32          // 正常停服标志
	origArgs        []string       // 程序原始运行参数
	wg              sync.WaitGroup // 等待组
	xCmdObj         *XCmd          // 扩展Cmd
	childCmd        string         // 运行子进程命令 --child
	upgradeCmd      string         // 更新命名 --upgrade
	bootstrapArgs   string         // 引导参数 --bootstrap_args
	bootstrapLogDir string         // 引导日志
	pidFile         string         // PID文件
	tcpPorts        map[string]int // 业务逻辑层需要用的端口
}

// New 工厂方法
func New(childCmd, upgradeCmd, bootstrapArgs, bootstrapLogDir, pidFile string) *Daemon {
	return &Daemon{
		childCmd:        childCmd,
		upgradeCmd:      upgradeCmd,
		bootstrapArgs:   bootstrapArgs,
		bootstrapLogDir: bootstrapLogDir,
		pidFile:         pidFile,
	}
}

// Default 默认实现
func Default() *Daemon {
	return New("child",
		"upgrade",
		"bootstrap_args",
		"bootstrapLogs",
		"daemonPID")
}

// spawnChildProcess 生成孩子进程
func (object *Daemon) spawnChildProcess(tcpLnFiles map[string]*os.File) (xCmdObj *XCmd, err error) {
	// 构建启动参数
	args := make([]string, len(object.origArgs))
	copy(args, object.origArgs)
	args = append(args, "--"+object.childCmd)

	// 构建XCmd
	xCmdObj = NewXCmd(args[0], args[1:]...)

	// 赋值标准流
	xCmdObj.Stdin = os.Stdin
	xCmdObj.Stdout = os.Stdout
	xCmdObj.Stderr = os.Stderr

	// 填入fd
	tcpLnFds := make(map[string]int)
	for k, f := range tcpLnFiles {
		tcpLnFds[k] = xCmdObj.AddFile(f).NextFd()
	}

	// 写入启动参数
	var raw []byte
	raw, err = json.Marshal(tcpLnFds)
	panicOnError(err)
	xCmdObj.Args = append(xCmdObj.Args,
		fmt.Sprintf("--%s%s=", object.bootstrapArgs, string(raw)))

	// 启动子进程
	if err = xCmdObj.Start(); nil != err {
		glog.Error(err)
		return
	}

	return
}

// Bootstrap 引导
func (object *Daemon) Bootstrap(tcpPorts map[string]int, //TCP端口
	logical func(tcpFds map[string]int), // 业务逻辑
	ready <-chan bool, // 准备好通道
) (err error) {
	runInChild := flag.Bool(object.childCmd, false, "run in child")
	runUpgrade := flag.Bool(object.upgradeCmd, false, "run upgrade")
	bootstrapArgs := flag.String(object.bootstrapArgs, "", "bootstrap args")
	flag.Parse()

	// 运行业务逻辑
	if nil != runInChild && *runInChild {
		// 检查运行参数
		if nil == bootstrapArgs || 0 >= len(*bootstrapArgs) {
			glog.Error("bootstrap argument is empty")
			return
		}

		// 获取通信对象
		object.xCmdObj = XCmdFromFd(3, 4)
		defer object.xCmdObj.Close()

		// 解析fd
		tcpFds := make(map[string]int)
		panicOnError(json.Unmarshal([]byte(*bootstrapArgs), tcpFds))

		// 调用业务逻辑
		go logical(tcpFds)

		// 等待准备好
		ok := <-ready
		if !ok {
			glog.Error("logical ready not ok")
			object.xCmdObj.ChildWrite([]byte(ReadyError))
		} else {
			object.xCmdObj.ChildWrite([]byte(ReadyOK))
		}
		return
	}

	// 运行更新程序
	if nil != runUpgrade && *runUpgrade {
		glog.Info("upgrade app")

		// 读取PID
		raw, err := ioutil.ReadFile(object.pidFile)
		if nil != err {
			glog.Error(err)
			return
		}

		var pid int
		if pid, err = strconv.Atoi(string(raw)); nil != err {
			glog.Error(err)
			return
		}

		// 查找进程
		var p *os.Process
		if p, err = os.FindProcess(pid); nil != err {
			glog.Error(err)
			return
		}

		// 通知更新
		if nil != p {
			if err = p.Signal(syscall.SIGUSR2); nil != err {
				glog.Error(err)
				return
			}
		}
		return
	}

	// 保存原始运行参数
	object.origArgs = make([]string, len(os.Args))
	copy(object.origArgs, os.Args)

	// 写进程PID
	panicOnError(ioutil.WriteFile(object.pidFile,
		[]byte(strconv.Itoa(os.Getpid())),
		0666))

	// 清空日志文件
	os.RemoveAll(object.bootstrapLogDir)
	os.Mkdir(object.bootstrapLogDir, 0777)

	// 侦听端口
	tcpLnFiles := make(map[string]*os.File)
	for uniqueName, port := range tcpPorts {
		var ln *net.TCPListener
		ln, err = net.ListenTCP("tcp", &net.TCPAddr{
			IP:   net.ParseIP("0.0.0.0"),
			Port: port,
		})
		if nil != err {
			glog.Error(err)
			return
		}

		var lnFile *os.File
		lnFile, err = ln.File()
		if nil != err {
			glog.Error(err)
			return
		}

		tcpLnFiles[uniqueName] = lnFile
	}

	// 启动进程
	object.xCmdObj, err = object.spawnChildProcess(tcpLnFiles)
	if nil != err {
		glog.Error(err)
		return
	}

	defer object.xCmdObj.Close()

	// 等待子进程启动成功
	ok := false
	if err = object.xCmdObj.ParentRead(func(raw []byte) bool {
		request := string(raw)
		switch request {
		case ReadyOK:
			glog.Info("child ready ok")
			ok = true
			return false

		case ReadyError:
			glog.Error("child ready error")
			return false

		default:
			return true
		}
	}); nil != err {
		glog.Error(err)
	}

	// 启动子进程失败
	if !ok {
		return
	}

	// 等待进程退出
	object.wg.Add(1)
	go func() {
		defer object.wg.Done()
		if err = object.xCmdObj.Wait(); nil != err {
			glog.Error(err)
		}
		if 0 == atomic.LoadInt32(&object.killedFlag) {
			glog.Errorf("child: %d done", object.xCmdObj.Process.Pid)
			//TODO 子进程意外死亡,重启子进程
		} else {
			glog.Infof("child: %d done", object.xCmdObj.Process.Pid)
		}
	}()

	// 等待信号
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh)
loop:
	for s := range signalCh {
		switch s {
		case syscall.SIGINT, syscall.SIGTERM:
			glog.Info("notify child exit")

			// 设置主动停服标志
			atomic.StoreInt32(&object.killedFlag, 1)
			// 发送信号，停止子进程
			if err = object.xCmdObj.Process.Kill(); nil != err {
				glog.Error(err)
			}
			object.wg.Wait()
			break loop

		case syscall.SIGUSR2:
			glog.Infof("notify upgrade app")

			var newXCmdObj *XCmd
			newXCmdObj, err = object.spawnChildProcess(tcpLnFiles)
			if nil != err {
				glog.Error(err)
				return
			}
			// 等待子进程启动成功
			ok := false
			if err = object.xCmdObj.ParentRead(func(raw []byte) bool {
				request := string(raw)
				switch request {
				case ReadyOK:
					glog.Info("child ready ok")
					ok = true
					return false

				case ReadyError:
					glog.Error("child ready error")
					return false

				default:
					return true
				}
			}); nil != err {
				glog.Error(err)
			}

			// 启动子进程失败
			if !ok {
				break loop
			}

			glog.Info("notify old child exit")

			object.xCmdObj.Process.Kill()
			object.wg.Wait()

			glog.Info("notify old child exit")

			object.xCmdObj.Close()

			glog.Infof("wait new child")
			object.xCmdObj = newXCmdObj
			object.wg.Add(1)
			go func() {
				defer object.wg.Done()
				if err = object.xCmdObj.Wait(); nil != err {
					glog.Error(err)
				}
				if 0 == atomic.LoadInt32(&object.killedFlag) {
					glog.Errorf("child: %d done", object.xCmdObj.Process.Pid)
					//TODO 子进程意外死亡,重启子进程
				} else {
					glog.Infof("child: %d done", object.xCmdObj.Process.Pid)
				}
			}()
		}
	}

	glog.Info("app exited")
	return
}
