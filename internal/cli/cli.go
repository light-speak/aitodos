package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/light-speak/aitodos/internal/daemon"
	"github.com/light-speak/aitodos/internal/project"
)

// Run 执行 ATS CLI 并返回进程退出码。
func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "init":
		err = runInit(ctx, args[1:], stdout, stderr)
	case "start":
		err = runStart(ctx, args[1:], stdout, stderr)
	case "status":
		err = runStatus(ctx, args[1:], stdout, stderr)
	case "stop":
		err = runStop(ctx, args[1:], stdout, stderr)
	case "open":
		err = runOpen(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "未知命令: %s\n", args[0])
		printUsage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}
	return 0
}

func runInit(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats init 不接受位置参数")
	}
	directory, err := os.Getwd()
	if err != nil {
		return err
	}
	currentProject, created, err := project.Initialize(ctx, directory)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(stdout, "已初始化 ATS 项目: %s\n", currentProject.Root)
	} else {
		fmt.Fprintf(stdout, "ATS 项目已经初始化: %s\n", currentProject.Root)
	}
	fmt.Fprintf(stdout, "数据库: %s\n", currentProject.Paths.Database)
	fmt.Fprintf(stdout, "项目并发: %d\n", currentProject.Local.Agent.MaxWorkers)
	return nil
}

func runStart(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("start", flag.ContinueOnError)
	flags.SetOutput(stderr)
	port := flags.Int("port", -1, "监听端口；默认读取 .ats/local.toml，0 表示随机端口")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("ats start 不接受位置参数")
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	selectedPort := currentProject.Local.Server.Port
	if *port != -1 {
		selectedPort = *port
	}
	if selectedPort < 0 || selectedPort > 65535 {
		return errors.New("--port 必须为 0 到 65535；0 表示使用随机端口")
	}
	return daemon.Serve(ctx, currentProject, "foreground", selectedPort, func(metadata daemon.Metadata) {
		printStarted(stdout, currentProject, metadata)
	})
}

func runStatus(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	metadata, running := daemon.Status(ctx, currentProject)
	if !running {
		fmt.Fprintf(stdout, "ATS 项目未运行: %s\n", currentProject.Root)
		return nil
	}
	fmt.Fprintf(stdout, "ATS 项目前台正在运行\n项目: %s\nPID: %d\nURL: %s\n",
		currentProject.Root,
		metadata.PID,
		metadata.URL,
	)
	return nil
}

func runStop(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("stop", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	stopped, err := daemon.Stop(ctx, currentProject)
	if err != nil {
		return err
	}
	if !stopped {
		fmt.Fprintln(stdout, "ATS 项目未运行")
		return nil
	}
	fmt.Fprintln(stdout, "ATS 项目运行进程已停止")
	return nil
}

func runOpen(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("open", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	currentProject, err := loadCurrentProject(ctx)
	if err != nil {
		return err
	}
	metadata, running := daemon.Status(ctx, currentProject)
	if !running {
		return errors.New("ATS 项目未运行，请先执行 ats start 并保持该终端开启")
	}
	if err := daemon.OpenBrowser(metadata.URL); err != nil {
		return err
	}
	fmt.Fprintln(stdout, metadata.URL)
	return nil
}

func loadCurrentProject(ctx context.Context) (*project.Project, error) {
	directory, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return project.Load(ctx, directory)
}

func printStarted(
	output io.Writer,
	currentProject *project.Project,
	metadata daemon.Metadata,
) {
	fmt.Fprintf(output, "ATS 项目前台已启动\n项目: %s\nPID: %d\nURL: %s\n保持当前终端开启，按 Ctrl+C 停止\n",
		currentProject.Root,
		metadata.PID,
		metadata.URL,
	)
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, `用法: ats <command>

命令:
  init                    初始化当前 Git 项目
  start [--port PORT]     前台启动当前项目，不会自动打开浏览器
  status                  查看当前项目运行状态
  open                    打开当前项目页面
  stop                    停止当前项目运行进程`)
}
